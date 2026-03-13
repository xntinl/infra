# 1. Frame Parsing

<!--
difficulty: insane
concepts: [http2, frame-parsing, binary-protocol, frame-types, frame-header, data-frame, headers-frame, settings-frame, wire-format]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [33-tcp-udp-and-networking, 19-io-and-filesystem, 12-encoding-and-serialization]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Sections 33 (TCP/UDP networking), 19 (I/O), and 12 (encoding/serialization) or equivalent experience
- Familiarity with binary protocol design and big-endian byte ordering

## Learning Objectives

- **Design** a frame parser and serializer for the HTTP/2 binary framing layer that handles all nine standard frame types
- **Create** a high-performance frame reader and writer that operates on raw TCP connections with correct length-prefix parsing and error detection
- **Evaluate** the design trade-offs in HTTP/2's binary framing format compared to HTTP/1.1's text-based format, including parsing performance, extensibility, and debuggability

## The Challenge

HTTP/2 replaces HTTP/1.1's text-based message format with a binary framing layer. Every piece of data exchanged over an HTTP/2 connection -- headers, body data, settings, flow control signals, error notifications -- is encoded as a binary frame with a fixed 9-byte header followed by a variable-length payload. Understanding and implementing this framing layer is the foundation for everything else in HTTP/2.

You will build a complete HTTP/2 frame parser and serializer from scratch. The frame header contains three fields: length (24 bits), type (8 bits), flags (8 bits), reserved (1 bit), and stream identifier (31 bits). You must implement parsing and serialization for all nine frame types defined in RFC 9113: DATA, HEADERS, PRIORITY, RST_STREAM, SETTINGS, PUSH_PROMISE, PING, GOAWAY, and WINDOW_UPDATE. Each frame type has its own payload format with specific fields, and some types have flags that modify the payload interpretation (e.g., END_STREAM, END_HEADERS, PADDED).

The challenge is getting the binary parsing exactly right: big-endian byte ordering, bitfield extraction, padding handling, and strict validation of frame semantics (e.g., SETTINGS frames must have stream ID 0, SETTINGS ACK must have zero-length payload). Incorrect parsing leads to protocol errors that are extremely difficult to debug because the connection state desynchronizes.

## Requirements

1. Define a `FrameHeader` struct with fields: `Length uint32` (24-bit), `Type FrameType`, `Flags FrameFlags`, `StreamID uint32` (31-bit, masking the reserved bit)
2. Define a `FrameType` enum with constants for all nine HTTP/2 frame types: `DATA (0x0)`, `HEADERS (0x1)`, `PRIORITY (0x2)`, `RST_STREAM (0x3)`, `SETTINGS (0x4)`, `PUSH_PROMISE (0x5)`, `PING (0x6)`, `GOAWAY (0x7)`, `WINDOW_UPDATE (0x8)`
3. Define `FrameFlags` as a byte type with named constants for: `END_STREAM (0x1)`, `ACK (0x1)`, `END_HEADERS (0x4)`, `PADDED (0x8)`, `PRIORITY_FLAG (0x20)`
4. Implement a `FrameReader` that reads from an `io.Reader`, parses the 9-byte header, reads the payload of `Length` bytes, and returns typed frame structs
5. Implement a `FrameWriter` that serializes typed frame structs into their wire format and writes them to an `io.Writer`
6. Implement typed frame structs for each frame type with their specific payload fields: `DataFrame` (pad length, data), `HeadersFrame` (pad length, exclusive flag, stream dependency, weight, header block fragment), `SettingsFrame` (list of setting ID-value pairs), `RSTStreamFrame` (error code), `PingFrame` (8-byte opaque data), `GoawayFrame` (last stream ID, error code, debug data), `WindowUpdateFrame` (window size increment), `PushPromiseFrame` (promised stream ID, header block fragment), `PriorityFrame` (exclusive, stream dependency, weight)
7. Implement padding handling for DATA, HEADERS, and PUSH_PROMISE frames: when the PADDED flag is set, the first byte of the payload is the pad length, and that many bytes at the end of the payload are padding
8. Implement frame validation: reject frames with invalid stream IDs for their type (e.g., SETTINGS on non-zero stream), payloads exceeding `SETTINGS_MAX_FRAME_SIZE`, and invalid flag combinations
9. Enforce a configurable maximum frame size (default 16384 bytes per RFC 9113) and return a `FRAME_SIZE_ERROR` when a frame exceeds the limit
10. Implement the connection preface: the client sends `PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n` followed by a SETTINGS frame, and the server must validate this exact byte sequence
11. Write round-trip tests that serialize frames, parse them back, and verify all fields match for every frame type

## Hints

- Use `binary.BigEndian.Uint32` and `binary.BigEndian.PutUint32` for 32-bit field parsing, but remember that the length field is only 24 bits: `(buf[0]<<16 | buf[1]<<8 | buf[2])`
- For the stream ID, mask the reserved bit: `binary.BigEndian.Uint32(buf) & 0x7FFFFFFF`
- Use `io.ReadFull` to read exactly the number of bytes specified by the frame length -- partial reads from TCP connections are common and `io.ReadFull` handles them correctly
- For frame validation, create a `ConnectionError` type with an HTTP/2 error code field that can be used to send a GOAWAY frame
- Pre-allocate a `[9]byte` buffer for header reading to avoid per-frame allocation
- For SETTINGS frames, the payload is a sequence of 6-byte entries: 2 bytes for the setting identifier and 4 bytes for the value
- Use a `bufio.Writer` wrapping the `io.Writer` in `FrameWriter` to batch small frame writes into fewer syscalls

## Success Criteria

1. The frame reader correctly parses all nine frame types from their wire format
2. The frame writer correctly serializes all nine frame types to their wire format
3. Round-trip serialization and parsing produces identical frame values for every frame type
4. Padding is correctly handled for DATA, HEADERS, and PUSH_PROMISE frames
5. Frame validation rejects frames with invalid stream IDs, oversized payloads, and invalid flag combinations
6. The connection preface is correctly validated
7. Maximum frame size enforcement returns FRAME_SIZE_ERROR for oversized frames
8. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 9113 - HTTP/2](https://httpwg.org/specs/rfc9113.html#FrameHeader) -- the authoritative specification for HTTP/2 frame format
- [RFC 9113 - Frame Definitions](https://httpwg.org/specs/rfc9113.html#FrameTypes) -- detailed specification of each frame type's payload format
- [Go encoding/binary package](https://pkg.go.dev/encoding/binary) -- big-endian binary encoding for frame serialization
- [Go x/net/http2 source](https://cs.opensource.google/go/x/net/+/master:http2/frame.go) -- reference implementation to study (but not use)
- [HTTP/2 explained (Daniel Stenberg)](https://daniel.haxx.se/http2/) -- accessible introduction to HTTP/2 concepts

## What's Next

Continue to [HPACK Header Compression](../02-hpack-header-compression/02-hpack-header-compression.md) where you will implement the header compression algorithm used by HTTP/2 to efficiently encode headers.

## Summary

- HTTP/2 uses a binary framing layer with a fixed 9-byte header containing length, type, flags, and stream identifier
- All nine frame types have distinct payload formats with specific validation rules
- Big-endian byte ordering and bitfield extraction require careful implementation to avoid protocol desynchronization
- Padding support adds complexity to DATA, HEADERS, and PUSH_PROMISE frames
- Frame size enforcement prevents memory exhaustion from malicious or misconfigured peers
- The connection preface establishes the HTTP/2 protocol and must be validated byte-for-byte
