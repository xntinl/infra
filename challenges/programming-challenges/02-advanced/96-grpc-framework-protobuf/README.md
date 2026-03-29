<!-- difficulty: advanced -->
<!-- category: databases-time-series-tools -->
<!-- languages: [go, rust] -->
<!-- concepts: [protobuf-encoding, http2-framing, rpc-framework, service-definition, streaming, deadline-propagation] -->
<!-- estimated_time: 25-35 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [binary-encoding, tcp-sockets, http2-basics, concurrency, trait-interfaces] -->

# Challenge 96: gRPC Framework with Protobuf

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Binary data encoding: variable-length integers, big-endian/little-endian byte order
- TCP socket programming and HTTP/2 concepts (streams, frames, multiplexing)
- Concurrency primitives: goroutines/channels (Go), async/tokio (Rust)
- Interface-based design (Go interfaces, Rust traits)
- Understanding of RPC semantics at a user level (request/response, error codes)

## Learning Objectives

- **Implement** Protocol Buffer binary wire format encoding and decoding (varints, length-delimited fields, zigzag encoding)
- **Design** an HTTP/2 transport layer with connection framing, stream multiplexing, and flow control
- **Build** a service definition parser that reads simplified `.proto` files and generates dispatch tables
- **Analyze** how deadline propagation and cancellation flow through an RPC chain
- **Evaluate** the trade-offs between gRPC's binary framing and text-based protocols like REST/JSON

## The Challenge

gRPC is the backbone of modern microservice communication. It combines Protocol Buffers (a compact binary serialization format) with HTTP/2 (a multiplexed binary transport) to deliver low-latency, strongly-typed RPCs. Every major cloud provider, database client, and service mesh speaks gRPC.

Your task is to build a simplified but functional gRPC framework from scratch. This means implementing three layers: the Protocol Buffer encoding/decoding engine, the HTTP/2 transport with framing, and the RPC dispatch layer that ties them together. You will support unary RPCs (single request, single response) and server-streaming RPCs (single request, stream of responses).

The Protocol Buffer wire format is deceptively simple -- every field is a tag-value pair where the tag encodes both the field number and wire type. But implementing it correctly requires handling varints (variable-length 64-bit integers), zigzag encoding for signed integers, length-delimited fields (strings, bytes, nested messages), and packed repeated fields. One off-by-one in varint decoding corrupts every subsequent field.

The HTTP/2 layer is where complexity escalates. gRPC frames messages with a 5-byte prefix (compressed flag + 4-byte big-endian length), then sends them as HTTP/2 DATA frames. You need to implement enough of the HTTP/2 spec to handle HEADERS, DATA, RST_STREAM, and GOAWAY frames. Full HTTP/2 compliance is not required -- focus on what gRPC actually uses.

Both Go and Rust implementations are required.

## Requirements

1. **Protocol Buffer wire format encoding/decoding**:
   - Varint encoding/decoding (LEB128, up to 64-bit)
   - Zigzag encoding for signed integers (`sint32`, `sint64`)
   - Wire types: varint (0), 64-bit fixed (1), length-delimited (2), 32-bit fixed (5)
   - Field tag encoding: `(field_number << 3) | wire_type`
   - Support types: `int32`, `int64`, `uint32`, `uint64`, `sint32`, `sint64`, `bool`, `string`, `bytes`, `float`, `double`
   - Nested message encoding (length-delimited)
   - Packed repeated fields
   - Unknown field preservation (skip and store raw bytes)

2. **Service definition parsing**:
   - Parse simplified `.proto`-like syntax:
     ```
     message SearchRequest {
       string query = 1;
       int32 page = 2;
       int32 per_page = 3;
     }
     message SearchResult {
       string url = 1;
       string title = 2;
     }
     service SearchService {
       rpc Search (SearchRequest) returns (SearchResult);
       rpc StreamResults (SearchRequest) returns (stream SearchResult);
     }
     ```
   - Extract message definitions with field names, types, and numbers
   - Extract service definitions with method names, request/response types, and streaming flags

3. **HTTP/2 transport layer**:
   - Connection preface (`PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n`)
   - Frame encoding/decoding: 9-byte header (length:3, type:1, flags:1, stream-id:4) + payload
   - SETTINGS frame exchange (initial handshake)
   - HEADERS frame with HPACK-lite (static table only, no Huffman, no dynamic table)
   - DATA frames with gRPC length-prefixed messages (1-byte compressed flag + 4-byte big-endian length + message)
   - RST_STREAM for error signaling
   - Stream multiplexing: odd-numbered client-initiated streams

4. **RPC dispatch**:
   - Server: register service implementations, listen on TCP, accept connections, dispatch incoming RPCs to handlers
   - Client: connect to server, send unary and streaming RPCs, receive responses
   - Unary RPC: send one request message, receive one response message
   - Server-streaming RPC: send one request, receive multiple response messages terminated by trailers

5. **Error handling and metadata**:
   - gRPC status codes: OK (0), Cancelled (1), Unknown (2), InvalidArgument (3), NotFound (5), Internal (13), Unavailable (14)
   - Status transmitted via `grpc-status` and `grpc-message` trailers
   - Deadline propagation: client sets deadline, server checks remaining time, returns DEADLINE_EXCEEDED if expired
   - Request metadata (key-value headers)

## Hints

1. Start with the protobuf layer in isolation. Write encode/decode for varints first -- every other type depends on them. A varint uses 7 bits per byte with the MSB as a continuation flag. Test with known values: `150` encodes as `[0x96, 0x01]`, `300` as `[0xAC, 0x02]`.

2. For HTTP/2 framing, you do not need a full HTTP/2 implementation. gRPC uses a narrow subset: connection preface, SETTINGS (can be empty), HEADERS (with a simplified pseudo-header set: `:method POST`, `:path /service/method`, `content-type application/grpc`), and DATA frames. WINDOW_UPDATE can be acknowledged but flow control enforcement can be simplified.

3. HPACK is the hardest part of HTTP/2 to implement fully. For this challenge, use the static table for well-known headers and send everything else as literal-without-indexing (byte `0x00` + name + value, both length-prefixed). This is valid HPACK that any decoder can read.

4. For service dispatch, build a registry mapping `/ServiceName/MethodName` paths to handler functions. The handler receives the decoded request bytes and returns response bytes (or a stream of response bytes for server-streaming). The framework handles framing; handlers deal only with protobuf-encoded payloads.

5. Deadline propagation: the client sends a `grpc-timeout` header (e.g., `100m` for 100 milliseconds, `5S` for 5 seconds). The server parses this and creates a context with deadline. If the handler does not complete before the deadline, the server sends status DEADLINE_EXCEEDED. In Go, use `context.WithDeadline`. In Rust, use `tokio::time::timeout`.

## Acceptance Criteria

- [ ] Varint encoding/decoding handles values from 0 to 2^63-1 correctly
- [ ] Zigzag encoding/decoding handles negative values: `-1` -> `1`, `-2` -> `3`, `1` -> `2`
- [ ] All protobuf wire types encode and decode correctly (round-trip test)
- [ ] Nested message encoding produces valid length-delimited fields
- [ ] Packed repeated fields encode multiple values in a single length-delimited field
- [ ] Service definition parser extracts messages and services from `.proto`-like input
- [ ] HTTP/2 connection handshake completes (preface + SETTINGS exchange)
- [ ] HEADERS and DATA frames encode/decode correctly with stream IDs
- [ ] gRPC length-prefixed message framing works (5-byte prefix + payload)
- [ ] Unary RPC round-trip: client sends request, server processes, client receives response
- [ ] Server-streaming RPC: client receives multiple messages followed by trailers
- [ ] Error codes propagate correctly via trailers
- [ ] Deadline propagation: expired deadline returns DEADLINE_EXCEEDED
- [ ] Both Go and Rust implementations interoperate (Go client -> Rust server and vice versa)
- [ ] All tests pass (`go test ./...` and `cargo test`)

## Research Resources

- [Protocol Buffers Encoding Guide](https://protobuf.dev/programming-guides/encoding/) -- official wire format specification
- [gRPC over HTTP/2 specification](https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md) -- the gRPC wire protocol
- [HTTP/2 RFC 9113](https://www.rfc-editor.org/rfc/rfc9113) -- frame format and connection semantics
- [HPACK RFC 7541](https://www.rfc-editor.org/rfc/rfc7541) -- header compression (Section 6 for encoding rules)
- [gRPC Status Codes](https://grpc.io/docs/guides/status-codes/) -- canonical error code definitions
- [Varint encoding (Wikipedia)](https://en.wikipedia.org/wiki/LEB128) -- LEB128 format used by protobuf
- [tonic (Rust gRPC)](https://github.com/hyperium/tonic) -- production Rust gRPC for architectural reference
- [google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc) -- official Go gRPC implementation
