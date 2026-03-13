# 20. Custom Wire Protocol

<!--
difficulty: insane
concepts: [wire-protocol, binary-framing, message-codec, length-prefix, protocol-versioning, backward-compatibility]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [building-a-line-based-protocol, tcp-server-and-client, concurrent-tcp-server, connection-pooling-implementation]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Line-Based Protocol and Connection Pooling exercises
- Experience with `encoding/binary`, `io.Reader`/`io.Writer`, and binary data layout
- Understanding of protocol design trade-offs (fixed vs variable length, versioning, extensibility)

## Learning Objectives

- **Create** a custom binary wire protocol with versioning, message types, and length-prefixed framing
- **Design** a codec layer that encodes and decodes structured messages to/from byte streams
- **Evaluate** trade-offs between protocol simplicity, extensibility, and backward compatibility

## The Challenge

Every networked system speaks a protocol. HTTP, gRPC, Redis, Kafka -- they all define precise byte-level formats for messages. When existing protocols do not fit your needs (custom RPC, game networking, IoT telemetry), you design your own wire protocol.

Your task is to design and implement a custom binary wire protocol for a key-value store. The protocol must support multiple operations (GET, SET, DELETE, SUBSCRIBE), handle variable-length keys and values, include a protocol version for backward compatibility, and support request/response correlation via request IDs. You will build both the codec (encoder/decoder) and a working client/server that speak this protocol over TCP.

## Requirements

1. Design a binary frame format with: magic bytes (2 bytes for protocol identification), protocol version (1 byte), flags (1 byte), message type (1 byte), request ID (4 bytes), payload length (4 bytes), payload, and a CRC32 checksum (4 bytes) of the entire frame excluding the checksum itself
2. Implement at least five message types: SET request, GET request, DELETE request, response (success/error), and SUBSCRIBE notification
3. Implement a `Codec` that reads and writes framed messages from any `io.ReadWriter`, handling partial reads and writes correctly
4. Add protocol version negotiation: the client sends its version in the first frame; the server responds with the highest mutually supported version
5. Support variable-length keys (up to 64KB) and values (up to 16MB) using length-prefixed encoding
6. Implement request/response correlation: each request carries a unique ID, the response echoes it
7. Build a TCP server that decodes incoming frames, dispatches to handlers by message type, and sends encoded responses
8. Build a TCP client that sends requests, matches responses by request ID, and supports concurrent in-flight requests
9. Handle malformed frames: reject frames with invalid magic bytes, unsupported versions, payload length exceeding maximum, or CRC checksum failures
10. Write tests that verify encode/decode round-trip, CRC validation, version negotiation, and malformed frame rejection

## Hints

- Use `encoding/binary.BigEndian` for all multi-byte integers to ensure portability
- Implement `ReadFrame` using `io.ReadFull` to handle TCP's stream nature (partial reads are normal)
- For concurrent request/response correlation on the client, use a `sync.Map` of `requestID -> chan Response`
- Magic bytes help detect protocol mismatches early (e.g., connecting an HTTP client to your protocol port)
- CRC32 with `hash/crc32.NewIEEE()` catches corruption from truncated or garbled frames
- Consider implementing a `bufio.Writer` wrapper with explicit `Flush()` to batch small writes

## Success Criteria

1. Frames encode and decode correctly in round-trip tests for all message types
2. CRC32 checksum detects corrupted frames (flip a bit, verify rejection)
3. Invalid magic bytes and unsupported versions are rejected with descriptive errors
4. The server processes GET, SET, and DELETE operations over the custom protocol
5. Request/response correlation works correctly with at least 10 concurrent in-flight requests
6. Payload lengths up to 16MB encode and decode correctly
7. Version negotiation selects the highest mutually supported version
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Redis RESP protocol](https://redis.io/docs/reference/protocol-spec/) -- example of a text-based wire protocol
- [Protocol Buffers Wire Format](https://protobuf.dev/programming-guides/encoding/) -- binary encoding with varint, length-delimited fields
- [Apache Kafka Protocol](https://kafka.apache.org/protocol.html) -- length-prefixed binary framing with versioned APIs
- [hash/crc32](https://pkg.go.dev/hash/crc32) -- Go's CRC32 implementation
- [encoding/binary](https://pkg.go.dev/encoding/binary) -- big-endian/little-endian integer encoding

## What's Next

Continue to [21 - TCP Load Balancer](../21-tcp-load-balancer/21-tcp-load-balancer.md) to build a load balancer that distributes TCP connections across multiple backends.

## Summary

- Wire protocols define the exact byte layout for communication between client and server
- Length-prefixed framing solves TCP's stream boundary problem (you never know where one message ends and the next begins)
- Magic bytes identify the protocol and catch mismatched connections early
- CRC checksums detect data corruption in transit
- Protocol versioning enables backward-compatible evolution without breaking existing clients
- Request ID correlation enables multiplexing multiple in-flight requests over a single connection
