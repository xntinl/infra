<!--
type: reference
difficulty: advanced
section: [08-networking-and-security]
concepts: [length-prefix-framing, delimiter-framing, multiplexing, backpressure, protocol-versioning, binary-protocol, flow-control]
languages: [go, rust]
estimated_reading_time: 60-75 min
bloom_level: analyze
prerequisites: [tcp-ip-fundamentals, io-models, goroutines-channels, rust-async-tokio]
papers: [Ford & Susarla 2008 — Breaking Up is Hard To Do: Security and Functionality in a Commodity Hypervisor, Paxson 1999 — End-to-End Internet Packet Dynamics]
industry_use: [grpc, redis-resp3, kafka-protocol, postgresql-wire, memcached-text-protocol]
language_contrast: high
-->

# Protocol Design

> Every byte layout decision you make in a wire protocol is permanent once you have
> deployed clients you do not control — get the framing, versioning, and error semantics
> right before shipping v1.

## Mental Model

Protocol design is an exercise in defining the contract between two processes that may
be written in different languages, running on different operating systems, at different
versions, communicating over an unreliable, reordering, potentially adversarial network.
The decisions you make in the first version are load-bearing: changing framing, field
widths, or error codes after deployment requires coordinated upgrades of all clients.

The first and most important decision is framing: how does a receiver know where one
message ends and the next begins? TCP is a byte stream — it provides no message
boundaries. Two approaches dominate production protocols:

**Length-prefix framing**: each message is preceded by a fixed-width integer that gives
the byte count of the message body. The receiver reads the header (e.g., 4 bytes), parses
the length, then reads exactly that many bytes. This is unambiguous, efficient, and safe.
gRPC, Kafka, PostgreSQL wire protocol, and Redis RESP3 (for bulk strings) all use it.

**Delimiter framing**: messages are terminated by a special byte sequence (e.g., `\r\n`,
`\n`, or a null byte). HTTP/1.1 headers use `\r\n` as a line terminator and `\r\n\r\n`
as the header/body separator; the body length is either given by `Content-Length` or by
chunked transfer encoding. The problem: if a message contains the delimiter byte sequence,
you need escaping. Escaping is a source of ambiguity, security vulnerabilities (injection
attacks), and performance overhead. HTTP/1.1's hybrid approach (delimiter for headers,
length-prefix for body) is widely considered a design mistake that HTTP/2 and HTTP/3
corrected by moving to full binary framing.

Backpressure is the second critical concept. If a producer sends faster than a consumer
can process, buffers grow unboundedly until the process OOMs. TCP's flow control (receiver
advertises a window size) handles the network-level backpressure, but application-level
protocols need their own backpressure signal. A streaming RPC protocol that sends 1000
responses before the client acknowledges any will either exhaust memory or drop messages.
HTTP/2's stream flow control window and gRPC's flow control credits are application-level
backpressure mechanisms layered on top of TCP's.

## Core Concepts

### Length-Prefix vs Delimiter Framing

```
Length-prefix (safe):
+--------+------ message bytes ------+
|  uint32|      N bytes of body      |
+--------+---------------------------+

Delimiter (fragile):
+------ bytes ------+----+
| body bytes (no \n)|0x0A|
+-------------------+----+

Hybrid (HTTP/1.1 headers):
GET /foo HTTP/1.1\r\n
Host: example.com\r\n
\r\n
[body: length from Content-Length or chunked encoding]
```

Length-prefix has one attack surface: an adversary can send a large length value (e.g.,
2^32 - 1) to cause the receiver to allocate 4GB. Always validate length against a
configured maximum before allocating.

### Multiplexing and Connection Reuse

HTTP/1.1 is one request-at-a-time per connection (pipelining exists but is broken in
practice). HTTP/2 multiplexes multiple streams over one TCP connection using stream IDs.
The core data structure is a stream table: a map from stream ID to per-stream state
(headers, body buffer, flow control window, RST state).

Head-of-line (HOL) blocking: in HTTP/1.1, a large response blocks all subsequent
responses. HTTP/2 solves HOL blocking at the application layer (streams interleave byte-
by-byte) but reintroduces it at the TCP layer — a dropped TCP segment stalls all streams
until retransmitted. QUIC solves both by multiplexing streams over UDP with per-stream
loss recovery.

### Protocol Versioning Strategies

**Version field in header (good)**: include a version byte in every frame header. Receiver
can reject unsupported versions with a clear error. Used by PostgreSQL wire protocol (major
version in startup packet) and gRPC (content-type: `application/grpc+proto`).

**Version negotiation at connection (good)**: client sends supported versions, server
selects. Used by TLS (supported_versions extension), QUIC (QUIC version negotiation
packet), and MQTT (protocol version byte in CONNECT).

**Implicit versioning via content-type (fragile)**: rely on HTTP Content-Type to
distinguish v1 and v2 APIs. Works for HTTP-based APIs but requires clients to correctly
set and servers to correctly parse Content-Type. No enforcement at the frame level.

**MUST NOT**: change the meaning of existing fields without a version bump. If field 3
is "message type" in v1 and "priority" in v2 without a version signal, you have an
undetectable incompatibility.

## Implementation: Go

```go
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
)

// FrameHeader is the fixed-size frame header for our binary protocol.
// Layout (10 bytes total):
//   [0:2]  magic (0xCAFE) — sanity check against stream corruption
//   [2:3]  version (1 byte)
//   [3:4]  message type (1 byte)
//   [4:8]  payload length (uint32, big-endian)
//   [8:10] reserved (2 bytes, must be zero — reserved for future flags)
const (
	headerMagic   = uint16(0xCAFE)
	headerSize    = 10
	maxPayloadLen = 16 * 1024 * 1024 // 16 MiB hard limit — reject before allocating
	protoVersion  = uint8(1)
)

type MessageType uint8

const (
	MsgRequest  MessageType = 0x01
	MsgResponse MessageType = 0x02
	MsgPing     MessageType = 0x03
	MsgPong     MessageType = 0x04
	MsgError    MessageType = 0xFF
)

// FrameHeader represents the parsed frame header.
type FrameHeader struct {
	Version    uint8
	Type       MessageType
	PayloadLen uint32
}

// Frame is a complete decoded message.
type Frame struct {
	FrameHeader
	Payload []byte
}

// writeFrame encodes and writes a frame to w.
// Length-prefix framing: header is always headerSize bytes, payload follows.
func writeFrame(w io.Writer, msgType MessageType, payload []byte) error {
	if len(payload) > maxPayloadLen {
		return fmt.Errorf("payload too large: %d > %d", len(payload), maxPayloadLen)
	}

	header := make([]byte, headerSize)
	binary.BigEndian.PutUint16(header[0:2], headerMagic)
	header[2] = protoVersion
	header[3] = byte(msgType)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	// header[8:10] reserved, already zero

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
	return nil
}

// readFrame reads and decodes the next frame from r.
// Key safety: we validate length before allocating to prevent OOM from malformed input.
func readFrame(r io.Reader) (*Frame, error) {
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	magic := binary.BigEndian.Uint16(header[0:2])
	if magic != headerMagic {
		return nil, fmt.Errorf("bad magic: 0x%04X (expected 0x%04X)", magic, headerMagic)
	}

	version := header[2]
	if version != protoVersion {
		return nil, fmt.Errorf("unsupported version %d (we support %d)", version, protoVersion)
	}

	msgType := MessageType(header[3])
	payloadLen := binary.BigEndian.Uint32(header[4:8])

	// CRITICAL: validate length before allocating.
	// Without this check, a malicious client sends 0xFFFFFFFF and causes a 4GB allocation.
	if payloadLen > maxPayloadLen {
		return nil, fmt.Errorf("payload too large: %d > %d", payloadLen, maxPayloadLen)
	}

	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
	}

	return &Frame{
		FrameHeader: FrameHeader{
			Version:    version,
			Type:       msgType,
			PayloadLen: payloadLen,
		},
		Payload: payload,
	}, nil
}

// EchoServer runs a simple length-prefix framing echo server.
// Demonstrates: frame reading, type dispatch, and proper error propagation.
func echoServer(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()
	log.Printf("echo server on %s", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	for {
		frame, err := readFrame(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return // clean close
			}
			log.Printf("read error from %s: %v", conn.RemoteAddr(), err)
			// Send error frame before closing.
			_ = writeFrame(conn, MsgError, []byte(err.Error()))
			return
		}

		switch frame.Type {
		case MsgRequest:
			if err := writeFrame(conn, MsgResponse, frame.Payload); err != nil {
				log.Printf("write response: %v", err)
				return
			}
		case MsgPing:
			if err := writeFrame(conn, MsgPong, nil); err != nil {
				log.Printf("write pong: %v", err)
				return
			}
		default:
			log.Printf("unknown message type 0x%02X from %s", frame.Type, conn.RemoteAddr())
		}
	}
}

// backpressureDemo illustrates an application-level backpressure scheme.
// The producer sends frames; the consumer sends ACK frames containing the last
// sequence number received. The producer blocks when unacknowledged frames exceed
// the window size (analogous to TCP flow control but at the application layer).
func backpressureDemo() {
	// This is a conceptual sketch — production implementations use
	// bidirectional streams (e.g., gRPC bidirectional streaming) or
	// credit-based schemes (e.g., AMQP prefetch_count).
	const windowSize = 100 // max unacknowledged frames in flight
	credits := make(chan struct{}, windowSize)
	for i := 0; i < windowSize; i++ {
		credits <- struct{}{}
	}

	producer := func(msgs [][]byte, w io.Writer) {
		for seq, msg := range msgs {
			<-credits // block until a credit is available
			frame := append([]byte{byte(seq >> 8), byte(seq)}, msg...)
			_ = writeFrame(w, MsgRequest, frame)
		}
	}
	_ = producer
}

func main() {
	go func() {
		if err := echoServer(":9000"); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	conn, err := net.Dial("tcp", "localhost:9000")
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	w := bufio.NewWriter(conn)
	if err := writeFrame(w, MsgRequest, []byte("hello world")); err != nil {
		log.Fatalf("write: %v", err)
	}
	if err := w.Flush(); err != nil {
		log.Fatalf("flush: %v", err)
	}

	frame, err := readFrame(conn)
	if err != nil {
		log.Fatalf("read: %v", err)
	}
	fmt.Printf("received type=%d payload=%q\n", frame.Type, frame.Payload)
}
```

### Go-specific considerations

**`bufio.Reader` for frame reading.** Reading a fixed-size header with `io.ReadFull`
performs one syscall per header and one per payload. Wrapping the connection in
`bufio.NewReader` batches reads, reducing syscall overhead for small frames.

**`io.ReadFull` semantics.** `io.ReadFull` reads exactly `len(buf)` bytes or returns an
error. It handles partial reads from the OS transparently. Never use `Read` directly for
protocol parsing — `Read` may return fewer bytes than requested even without an error.

**Goroutine-per-connection scaling.** Go's scheduler multiplexes goroutines over OS
threads, so goroutine-per-connection is practical up to ~100,000 concurrent connections
before memory pressure (each goroutine starts at 2KB, grows as needed) becomes an issue.
Above that threshold, use an event-loop model (multiplexed reads via `net.Poller`).

**`encoding/binary` byte order.** Always use `BigEndian` for network protocols (this is
the "network byte order" convention). `LittleEndian` works if both sides agree, but
big-endian is the default expectation for anyone reading a hex dump or Wireshark capture.

## Implementation: Rust

```rust
use std::io::{self, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use tokio::io::{AsyncReadExt, AsyncWriteExt};

const HEADER_MAGIC: u16 = 0xCAFE;
const HEADER_SIZE: usize = 10;
const MAX_PAYLOAD_LEN: u32 = 16 * 1024 * 1024; // 16 MiB
const PROTO_VERSION: u8 = 1;

#[repr(u8)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum MessageType {
    Request = 0x01,
    Response = 0x02,
    Ping = 0x03,
    Pong = 0x04,
    Error = 0xFF,
}

impl TryFrom<u8> for MessageType {
    type Error = ProtocolError;
    fn try_from(v: u8) -> Result<Self, Self::Error> {
        match v {
            0x01 => Ok(Self::Request),
            0x02 => Ok(Self::Response),
            0x03 => Ok(Self::Ping),
            0x04 => Ok(Self::Pong),
            0xFF => Ok(Self::Error),
            other => Err(ProtocolError::UnknownMessageType(other)),
        }
    }
}

#[derive(Debug, thiserror::Error)]
enum ProtocolError {
    #[error("io error: {0}")]
    Io(#[from] io::Error),
    #[error("bad magic: 0x{0:04X}")]
    BadMagic(u16),
    #[error("unsupported version {0}")]
    UnsupportedVersion(u8),
    #[error("payload too large: {0} > {}", MAX_PAYLOAD_LEN)]
    PayloadTooLarge(u32),
    #[error("unknown message type: 0x{0:02X}")]
    UnknownMessageType(u8),
}

#[derive(Debug)]
struct Frame {
    msg_type: MessageType,
    payload: Vec<u8>,
}

/// Encode and write a frame (synchronous, for demonstration).
fn write_frame(w: &mut impl Write, msg_type: MessageType, payload: &[u8]) -> Result<(), ProtocolError> {
    if payload.len() as u32 > MAX_PAYLOAD_LEN {
        return Err(ProtocolError::PayloadTooLarge(payload.len() as u32));
    }

    let mut header = [0u8; HEADER_SIZE];
    header[0..2].copy_from_slice(&HEADER_MAGIC.to_be_bytes());
    header[2] = PROTO_VERSION;
    header[3] = msg_type as u8;
    header[4..8].copy_from_slice(&(payload.len() as u32).to_be_bytes());
    // header[8..10] reserved, remain zero

    w.write_all(&header)?;
    if !payload.is_empty() {
        w.write_all(payload)?;
    }
    Ok(())
}

/// Read and decode the next frame.
/// Validates magic, version, and length before allocating the payload buffer.
fn read_frame(r: &mut impl Read) -> Result<Frame, ProtocolError> {
    let mut header = [0u8; HEADER_SIZE];
    r.read_exact(&mut header)?;

    let magic = u16::from_be_bytes([header[0], header[1]]);
    if magic != HEADER_MAGIC {
        return Err(ProtocolError::BadMagic(magic));
    }

    let version = header[2];
    if version != PROTO_VERSION {
        return Err(ProtocolError::UnsupportedVersion(version));
    }

    let msg_type = MessageType::try_from(header[3])?;
    let payload_len = u32::from_be_bytes([header[4], header[5], header[6], header[7]]);

    if payload_len > MAX_PAYLOAD_LEN {
        return Err(ProtocolError::PayloadTooLarge(payload_len));
    }

    let mut payload = vec![0u8; payload_len as usize];
    if payload_len > 0 {
        r.read_exact(&mut payload)?;
    }

    Ok(Frame { msg_type, payload })
}

/// Async version using tokio for production use.
/// The sync version above is useful for tests and single-threaded servers.
async fn read_frame_async(r: &mut (impl AsyncReadExt + Unpin)) -> Result<Frame, ProtocolError> {
    let mut header = [0u8; HEADER_SIZE];
    r.read_exact(&mut header).await?;

    let magic = u16::from_be_bytes([header[0], header[1]]);
    if magic != HEADER_MAGIC {
        return Err(ProtocolError::BadMagic(magic));
    }

    let version = header[2];
    if version != PROTO_VERSION {
        return Err(ProtocolError::UnsupportedVersion(version));
    }

    let msg_type = MessageType::try_from(header[3])?;
    let payload_len = u32::from_be_bytes([header[4], header[5], header[6], header[7]]);

    if payload_len > MAX_PAYLOAD_LEN {
        return Err(ProtocolError::PayloadTooLarge(payload_len));
    }

    let mut payload = vec![0u8; payload_len as usize];
    if payload_len > 0 {
        r.read_exact(&mut payload).await?;
    }

    Ok(Frame { msg_type, payload })
}

fn main() {
    let listener = TcpListener::bind("127.0.0.1:9001").expect("bind");
    println!("Listening on :9001");

    // Accept one connection for demonstration
    if let Ok((mut stream, addr)) = listener.accept() {
        println!("Connection from {addr}");
        let mut reader = BufReader::new(stream.try_clone().expect("clone"));

        loop {
            match read_frame(&mut reader) {
                Ok(frame) => {
                    println!("Received {:?}: {} bytes", frame.msg_type, frame.payload.len());
                    let reply_type = match frame.msg_type {
                        MessageType::Request => MessageType::Response,
                        MessageType::Ping => MessageType::Pong,
                        _ => MessageType::Error,
                    };
                    if let Err(e) = write_frame(&mut stream, reply_type, &frame.payload) {
                        eprintln!("write error: {e}");
                        break;
                    }
                }
                Err(ProtocolError::Io(e)) if e.kind() == io::ErrorKind::UnexpectedEof => break,
                Err(e) => {
                    eprintln!("protocol error: {e}");
                    let _ = write_frame(&mut stream, MessageType::Error, e.to_string().as_bytes());
                    break;
                }
            }
        }
    }
}
```

### Rust-specific considerations

**`thiserror` for protocol error types.** Using `thiserror::Error` produces ergonomic,
`Display`-formatted error types that implement `std::error::Error`. Protocol errors are
a natural fit: each variant maps to a distinct protocol violation, and the error message
is human-readable without exposing internals.

**`read_exact` is the Rust equivalent of `io::ReadFull`.** Like Go, never use `read` for
protocol parsing — use `read_exact` which retries until the buffer is full or returns
`UnexpectedEof`.

**Async tokio vs sync `std::io`.** In production, use tokio's `AsyncReadExt::read_exact`
and `AsyncWriteExt::write_all`. The sync version is useful for tests (no async runtime
overhead) and single-threaded tools. The protocol logic is identical; only the async/await
keywords and trait bounds differ.

**`#[repr(u8)]` for message types.** Using `#[repr(u8)]` and `TryFrom<u8>` is the
idiomatic way to parse wire protocol discriminant bytes into Rust enums. `TryFrom` returns
`Err` on unknown values, making the "unknown type" case explicit rather than panicking.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Binary encoding | `encoding/binary` (stdlib) | `byteorder` or manual byte ops |
| Exact reads | `io.ReadFull` | `read_exact` |
| Buffered I/O | `bufio.NewReader` | `BufReader` |
| Error representation | `error` interface + wrapping | `thiserror` / `enum` variants |
| Async I/O | goroutines (implicit) | `tokio` / `async-std` (explicit) |
| Protocol state machine | Manual struct + switch | Enum variants + pattern matching |
| Per-connection concurrency | goroutine per conn (default) | `tokio::spawn` per conn |
| Max payload enforcement | Manual `if len > max` check | Same — no magic |
| Protocol versioning | Version byte in header | Same — `TryFrom` for type safety |
| Multiplexing | Manual stream table or HTTP/2 lib | `h2` crate or `quinn` |

## Production War Stories

**Redis RESP protocol evolution (v1 → RESP3, 2020).** Redis's original RESP protocol used
delimiter framing (lines ending in `\r\n`) for commands and simple replies, and length-
prefix framing for bulk strings. RESP3 added new data types (doubles, blobs, maps,
attributes) and a negotiation command (`HELLO 3`) that clients send to opt into the new
protocol. The backwards compatibility was achieved entirely through the version negotiation
step — old clients continue speaking RESP v2 without any changes.

**Kafka's protocol evolution.** Kafka's binary protocol has had 100+ versions across its
API keys since 2012. Every request includes a 2-byte API key, a 2-byte API version, and
a 4-byte correlation ID. Brokers and clients negotiate the maximum supported version for
each API key during session setup. This design has allowed protocol evolution without
breaking client compatibility for 12 years — possibly the most successful protocol
versioning strategy in open-source infrastructure.

**HTTP/1.1 pipelining failure.** HTTP/1.1 defined request pipelining (send multiple
requests without waiting for responses), but almost no browser enabled it by default
because of head-of-line blocking and incorrect proxy implementations. The problem was
fundamental to delimiter framing: you cannot interleave responses without either response
boundaries (which HTTP/1.1 does not have in the response stream) or sequence numbers.
HTTP/2 fixed this with binary framing and stream IDs.

**gRPC flow control in practice.** gRPC uses HTTP/2 stream flow control, where each side
advertises a receive window (default 64KB per stream, 1MB per connection). In a streaming
RPC that sends 1MB messages, the sender blocks after the first message until the receiver
reads and sends a WINDOW_UPDATE frame. In high-latency networks (200ms RTT, cross-region),
this causes significant throughput degradation. The fix is to increase the initial window
size in the gRPC channel options — but this trades latency for memory pressure.

## Security Analysis

**Payload length attack surface.** Length-prefix protocols are vulnerable to OOM attacks
if the receiver allocates before validating. Always validate the length against a
maximum before `make([]byte, n)`. The maximum should be derived from the protocol
specification, not from available system memory.

**Magic byte bypass.** A magic field (0xCAFE in our protocol) catches stream corruption
and misdirected connections but is not a security boundary. An attacker who can send
arbitrary bytes can produce the correct magic. Use authentication (HMAC over the frame,
or TLS) for security; use magic for debugging.

**Version downgrade attack.** If version negotiation accepts "use the minimum supported
version," an attacker can force a downgrade to an older, less secure version. Version
negotiation must be authenticated (part of a signed hello or protected by TLS) to be
meaningful.

**Delimiter injection.** Any protocol using delimiter framing must sanitize or escape
the delimiter in data. HTTP header injection (CRLF injection) is an attack class that
exploits the failure to sanitize `\r\n` in header values. Length-prefix framing eliminates
this class entirely.

## Common Pitfalls

1. **Allocating before validating the length field.** The single most common protocol
   parsing bug in length-prefix protocols. Always check `length <= maxAllowed` before
   `make([]byte, length)`.

2. **Using `Read` instead of `ReadFull`/`read_exact`.** `Read` returns as many bytes as
   are available, which may be less than requested. Protocol parsers that call `Read`
   and assume they get a full frame will silently parse garbage. Always use
   `io.ReadFull` / `read_exact`.

3. **Forgetting to flush buffered writers.** Wrapping a connection in `bufio.Writer`
   or `BufWriter` means data is buffered until `Flush`. A server that writes a response
   into a `bufio.Writer` and then reads the next request will deadlock: the client is
   waiting for the response (still in the buffer), the server is waiting for the next
   request.

4. **Using textual protocols for high-throughput paths.** Parsing text requires handling
   encoding, escape sequences, and variable-length fields. Binary protocols with fixed
   headers are 5–50x faster to parse. Use text protocols (JSON-over-HTTP) for
   human-friendly APIs; use binary protocols for service-to-service RPC.

5. **Changing field semantics without a version bump.** If field offsets or semantics
   change between versions and the version byte is not incremented, old clients will
   silently misparse new messages. This is often done "just for compatibility" and
   causes subtle bugs that only manifest when old and new clients coexist.

## Exercises

**Exercise 1** (30 min): Wireshark the Redis RESP protocol. Run a local Redis server
and `redis-cli`. In Wireshark, filter `tcp.port == 6379` and decode the traffic. Identify
where the RESP framing boundaries are: `+` for simple strings, `$<len>\r\n` for bulk
strings, `*<count>\r\n` for arrays. Compare this to the length-prefix framing in this
document.

**Exercise 2** (2–4h): Implement a simple in-memory key-value server that speaks the
binary protocol defined in this document over TCP. Support four message types: GET,
SET, DEL, and LIST (list all keys). Each message's payload is a JSON-encoded request or
response. Write a client that sends 10,000 SET operations and measures throughput.

**Exercise 3** (4–8h): Extend the server to support multiplexing: include a 4-byte
stream ID in the frame header. The server processes requests for different stream IDs
concurrently (using goroutines in Go or `tokio::spawn` in Rust). Measure the latency
improvement for 10 concurrent clients vs 10 sequential clients.

**Exercise 4** (8–15h): Add application-level backpressure to the multiplexing server.
Each client has a credit budget (e.g., 100 in-flight requests). The server sends a
CREDIT_UPDATE frame when it finishes processing a batch. The client blocks when credits
are exhausted. Implement in both Go and Rust. Benchmark with a fast producer (100µs/msg)
and a slow consumer (10ms/msg). Verify that the producer's send buffer does not grow
unboundedly.

## Further Reading

### Foundational Papers

- Clark, "The Design Philosophy of the DARPA Internet Protocols" (SIGCOMM 1988). The
  original paper explaining why TCP makes the design choices it does (stream, not message).
- Saltzer, Reed, Clark, "End-to-End Arguments in System Design" (ACM TOCS 1984). The
  canonical argument for why reliability must be implemented end-to-end, not in
  intermediate nodes.

### Books

- W. Richard Stevens, "UNIX Network Programming Vol. 1" (3rd ed.) — chapter 7 on socket
  options and chapter 14 on nonblocking I/O are particularly relevant to protocol
  implementation in C; the concepts transfer directly to Go and Rust.
- Doug Comer, "Internetworking with TCP/IP" — the chapter on application protocols and
  the design of remote procedure call systems.

### Production Code to Read

- Redis RESP protocol specification: `redis.io/docs/reference/protocol-spec/` — study
  how a real production protocol balances simplicity with expressiveness.
- Kafka protocol guide: `kafka.apache.org/protocol.html` — an exceptionally well-
  documented binary protocol with API versioning that has worked at scale for 12 years.
- gRPC wire format (`grpc.github.io/grpc/core/md_doc_wire_format.html`) — shows how
  HTTP/2 framing is used for gRPC, and how the 5-byte gRPC data frame header adds
  compression flags on top of HTTP/2's binary framing.

### Security Advisories / CVEs to Study

- CVE-2016-6210 (OpenSSH username enumeration) — timing difference in CRLF handling
  in a text protocol leaked whether a username existed.
- HTTP Request Smuggling (CL.TE / TE.CL, PortSwigger research) — exploits the
  ambiguity between `Content-Length` and `Transfer-Encoding` framing in HTTP/1.1.
  Demonstrates the cost of delimiter framing ambiguity at scale.
