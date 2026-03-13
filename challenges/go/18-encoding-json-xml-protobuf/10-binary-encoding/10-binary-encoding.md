# Exercise 10: Binary Encoding

**Difficulty:** Advanced | **Estimated Time:** 30 minutes | **Section:** 18 - Encoding

## Overview

Text formats like JSON are readable but inefficient. Binary encoding packs data tightly, producing smaller payloads and faster serialization. Go's `encoding/binary` package provides primitives for reading and writing fixed-size values, and `encoding/gob` offers a Go-native binary format. This exercise explores both, plus manual wire-format design.

## Prerequisites

- Byte slices and bit operations
- `io.Reader` / `io.Writer`
- Struct layout basics

## Problem

Build a binary packet encoder/decoder for a simplified network protocol:

### Packet Format

```
Offset  Size  Field
0       1     Version (uint8) -- always 1
1       1     Type (uint8) -- 1=Ping, 2=Data, 3=Ack
2       4     Sequence (uint32, big-endian)
6       8     Timestamp (int64, big-endian, Unix nanoseconds)
14      2     PayloadLen (uint16, big-endian)
16      N     Payload (variable-length bytes)
16+N    4     Checksum (uint32, CRC32 of bytes 0 through 16+N-1)
```

### Part 1: Manual binary encoding with `encoding/binary`

Write functions:

```go
func EncodePacket(p *Packet) ([]byte, error)
func DecodePacket(data []byte) (*Packet, error)
```

Use `binary.BigEndian.PutUint32`, `binary.BigEndian.Uint32`, etc. for each field. Compute CRC32 using `hash/crc32`.

Validate on decode:
- Version must be 1
- PayloadLen must match actual remaining bytes
- Checksum must match

### Part 2: `binary.Write` / `binary.Read`

Rewrite encoding/decoding using `binary.Write` and `binary.Read` with a fixed-size header struct:

```go
type PacketHeader struct {
    Version    uint8
    Type       uint8
    Sequence   uint32
    Timestamp  int64
    PayloadLen uint16
}
```

Write the header, then the payload, then the checksum.

### Part 3: `encoding/gob`

Encode the same packet data using `encoding/gob`. Compare the output size against your manual binary format and against JSON.

Print a table:

```
Format      Size (bytes)
Manual      <N>
Gob         <N>
JSON        <N>
```

## Hints

- `binary.BigEndian` is a `ByteOrder` value, not a package. Call methods on it: `binary.BigEndian.PutUint32(buf, val)`.
- For manual encoding, pre-allocate the buffer: `make([]byte, 16+len(payload)+4)`.
- `crc32.ChecksumIEEE(data)` returns a `uint32` checksum.
- `binary.Write(buf, binary.BigEndian, &header)` writes all fields of a fixed-size struct.
- `binary.Read(buf, binary.BigEndian, &header)` reads them back.
- `gob` requires `Register` for interface types but works directly with concrete structs.
- For fair comparison, encode the same data (same payload string) in all three formats.

## Verification Criteria

- Manual encode/decode round-trips correctly for all three packet types (Ping, Data, Ack)
- Checksum validation catches corrupted packets (flip a byte and verify decode fails)
- `binary.Write`/`binary.Read` produces identical bytes to manual encoding
- Size comparison shows manual binary is smallest, gob is mid-range, JSON is largest
- A Data packet with a 100-byte payload should be ~120 bytes in binary format

## Stretch Goals

- Add variable-length integer encoding (like protobuf varints) using `binary.PutUvarint`
- Implement a packet stream: write multiple packets to a buffer, read them back sequentially
- Add encryption: XOR the payload with a key before computing the checksum

## Key Takeaways

- `encoding/binary` provides low-level control over byte layout and endianness
- Manual binary encoding gives the smallest output and full control over the wire format
- `binary.Write`/`binary.Read` reduce boilerplate for fixed-size structs
- `encoding/gob` is Go-specific but handles complex types (maps, slices, interfaces) automatically
- Always include checksums or length fields in binary protocols for validation
