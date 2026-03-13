# 3. Type Punning

<!--
difficulty: advanced
concepts: [type-punning, bit-reinterpretation, unsafe-cast, binary-protocols, ieee754]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [unsafe-pointer-and-uintptr, unsafe-sizeof-alignof-offsetof, binary-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `unsafe.Pointer` conversions and memory layout
- Basic knowledge of binary data formats and IEEE 754 floating-point representation

## Learning Objectives

After completing this exercise, you will be able to:

- **Reinterpret** the raw bytes of one type as another type using `unsafe.Pointer`
- **Implement** zero-copy conversions between compatible memory layouts
- **Decode** binary protocol headers by casting byte slices to struct pointers
- **Identify** when type punning is safe versus when it causes undefined behavior

## Why Type Punning

Type punning reads memory written as one type and interprets it as another -- without copying or converting. This is how high-performance network stacks parse packet headers: instead of deserializing bytes into a struct field by field, you cast the byte buffer pointer directly to a struct pointer. The kernel does this. DPDK does this. When you need to process millions of packets per second, the difference between a zero-copy cast and a `binary.Read` matters.

In Go, type punning requires `unsafe.Pointer`. The rules are strict: the source and destination types must have compatible memory layouts (same size, compatible alignment). Violating these rules produces corrupted data or crashes.

## Step 1 -- Numeric Type Punning

```bash
mkdir -p ~/go-exercises/type-punning && cd ~/go-exercises/type-punning
go mod init type-punning
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"math"
	"unsafe"
)

func main() {
	// Pun int32 as [4]byte -- inspect the byte representation
	n := int32(0x01020304)
	bytes := *(*[4]byte)(unsafe.Pointer(&n))
	fmt.Printf("int32 0x%08x as bytes: %v\n", n, bytes)
	// On little-endian: [4 3 2 1], on big-endian: [1 2 3 4]

	// Pun float64 as uint64 to examine IEEE 754 bits
	pi := math.Pi
	bits := *(*uint64)(unsafe.Pointer(&pi))
	fmt.Printf("Pi = %f\n", pi)
	fmt.Printf("  sign: %d\n", bits>>63)
	fmt.Printf("  exponent: %d (biased)\n", (bits>>52)&0x7FF)
	fmt.Printf("  mantissa: 0x%013x\n", bits&0xFFFFFFFFFFFFF)

	// Verify against math.Float64bits (the safe alternative)
	safeBits := math.Float64bits(pi)
	fmt.Printf("  math.Float64bits matches: %v\n", bits == safeBits)

	// Pun int64 as two int32s
	big := int64(0x00000001_00000002)
	pair := *(*[2]int32)(unsafe.Pointer(&big))
	fmt.Printf("int64 0x%016x as two int32s: [0x%08x, 0x%08x]\n",
		big, pair[0], pair[1])
}
```

```bash
go run main.go
```

### Intermediate Verification

The byte order depends on architecture (little-endian on x86/ARM). The IEEE 754 inspection shows Pi's sign (0), biased exponent, and mantissa bits. The safe `math.Float64bits` produces identical results.

## Step 2 -- Struct-to-Struct Punning

Type-pun between structs with identical memory layout but different field names:

```go
package main

import (
	"fmt"
	"unsafe"
)

// Network byte order header (big-endian fields would need ntohl in practice)
type RawHeader struct {
	Version  uint8
	Type     uint8
	Length   uint16
	Sequence uint32
}

// Application-level interpretation
type MessageHeader struct {
	Proto    uint8
	Kind     uint8
	Size     uint16
	SeqNum   uint32
}

func main() {
	raw := RawHeader{
		Version:  2,
		Type:     0x10,
		Length:   1024,
		Sequence: 42,
	}

	// Both structs have identical layout: uint8, uint8, uint16, uint32
	msg := (*MessageHeader)(unsafe.Pointer(&raw))

	fmt.Printf("Raw:     Version=%d Type=0x%02x Length=%d Seq=%d\n",
		raw.Version, raw.Type, raw.Length, raw.Sequence)
	fmt.Printf("Message: Proto=%d Kind=0x%02x Size=%d SeqNum=%d\n",
		msg.Proto, msg.Kind, msg.Size, msg.SeqNum)

	// Verify sizes match
	fmt.Printf("\nRawHeader size:     %d\n", unsafe.Sizeof(raw))
	fmt.Printf("MessageHeader size: %d\n", unsafe.Sizeof(*msg))
}
```

## Step 3 -- Byte Slice to Struct (Zero-Copy Header Parsing)

This is the most practical use case -- parsing binary protocol data without copying:

```go
package main

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

type PacketHeader struct {
	SrcPort  uint16
	DstPort  uint16
	Length   uint16
	Checksum uint16
}

// Zero-copy: cast slice pointer to struct pointer
func parseHeaderUnsafe(data []byte) (*PacketHeader, error) {
	if len(data) < int(unsafe.Sizeof(PacketHeader{})) {
		return nil, fmt.Errorf("buffer too small: need %d, got %d",
			unsafe.Sizeof(PacketHeader{}), len(data))
	}
	return (*PacketHeader)(unsafe.Pointer(&data[0])), nil
}

// Safe alternative: copy bytes into struct
func parseHeaderSafe(data []byte) (PacketHeader, error) {
	var h PacketHeader
	if len(data) < 8 {
		return h, fmt.Errorf("buffer too small")
	}
	h.SrcPort = binary.LittleEndian.Uint16(data[0:2])
	h.DstPort = binary.LittleEndian.Uint16(data[2:4])
	h.Length = binary.LittleEndian.Uint16(data[4:6])
	h.Checksum = binary.LittleEndian.Uint16(data[6:8])
	return h, nil
}

func main() {
	// Simulate a packet buffer
	buf := make([]byte, 64)
	binary.LittleEndian.PutUint16(buf[0:2], 8080)  // src port
	binary.LittleEndian.PutUint16(buf[2:4], 443)   // dst port
	binary.LittleEndian.PutUint16(buf[4:6], 64)    // length
	binary.LittleEndian.PutUint16(buf[6:8], 0xABCD) // checksum

	// Zero-copy parse
	h, err := parseHeaderUnsafe(buf)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Unsafe parse: src=%d dst=%d len=%d chk=0x%04x\n",
		h.SrcPort, h.DstPort, h.Length, h.Checksum)

	// Modifying h modifies the original buffer (zero-copy!)
	h.SrcPort = 9090
	fmt.Printf("After modification, buf[0:2] = 0x%02x%02x\n", buf[1], buf[0])

	// Safe parse (independent copy)
	h2, _ := parseHeaderSafe(buf)
	fmt.Printf("Safe parse:   src=%d dst=%d len=%d chk=0x%04x\n",
		h2.SrcPort, h2.DstPort, h2.Length, h2.Checksum)
}
```

## Step 4 -- Benchmark: Punning vs Decoding

```go
package main

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

type Header struct {
	A uint32
	B uint32
	C uint64
	D uint16
	E uint16
}

var buf = make([]byte, 64)

func BenchmarkUnsafePun(b *testing.B) {
	var h *Header
	for i := 0; i < b.N; i++ {
		h = (*Header)(unsafe.Pointer(&buf[0]))
	}
	_ = h
}

func BenchmarkBinaryRead(b *testing.B) {
	var h Header
	for i := 0; i < b.N; i++ {
		h.A = binary.LittleEndian.Uint32(buf[0:4])
		h.B = binary.LittleEndian.Uint32(buf[4:8])
		h.C = binary.LittleEndian.Uint64(buf[8:16])
		h.D = binary.LittleEndian.Uint16(buf[16:18])
		h.E = binary.LittleEndian.Uint16(buf[18:20])
	}
	_ = h
}
```

```bash
go test -bench=. -benchmem
```

## Hints

- Type punning only works when source and destination have the same size and compatible layout
- Byte order matters: type punning gives you the machine's native byte order, not network byte order
- When punning a byte slice to a struct, the struct pointer aliases the slice's backing array -- modifications are shared
- Padding can cause subtle bugs: a struct with padding bytes may include garbage in those positions
- The safe alternatives (`encoding/binary`, `math.Float64bits`) should be preferred unless benchmarks prove the unsafe version is necessary
- `go vet` will not catch layout mismatches between punned types -- you must verify sizes yourself

## Verification

- Float64-to-uint64 punning matches `math.Float64bits` output
- Struct-to-struct punning preserves all field values when layouts match
- Byte-slice-to-struct punning gives zero-copy access (modifying through the struct changes the original bytes)
- Benchmark shows punning is significantly faster than `encoding/binary` field-by-field decoding
- Buffer-too-small check prevents out-of-bounds access

## What's Next

With type punning understood, the next exercise introduces cgo -- calling C code from Go, which is the primary production use case for `unsafe.Pointer`.

## Summary

Type punning reinterprets memory as a different type via `unsafe.Pointer` without copying data. It is used for IEEE 754 bit inspection, binary protocol header parsing, and zero-copy data access. The source and destination types must have identical memory layouts (size, alignment, field offsets). Punning byte slices to structs gives zero-copy reads but aliases the original memory. Always verify layout compatibility with `unsafe.Sizeof` and `unsafe.Offsetof`. Prefer safe alternatives (`encoding/binary`, `math.Float64bits`) unless benchmarks justify the unsafe approach.

## Reference

- [unsafe.Pointer](https://pkg.go.dev/unsafe#Pointer)
- [encoding/binary](https://pkg.go.dev/encoding/binary)
- [math.Float64bits](https://pkg.go.dev/math#Float64bits)
- [IEEE 754 floating-point](https://en.wikipedia.org/wiki/IEEE_754)
