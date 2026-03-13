# 9. Zero-Copy Deserialization

<!--
difficulty: insane
concepts: [zero-copy, unsafe-cast, binary-protocol, flatbuffer-style, alignment, endianness]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [type-punning, unsafe-slice-and-string, unsafe-sizeof-alignof-offsetof, binary-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of type punning, memory layout, alignment, and `unsafe.Slice`
- Familiarity with binary data formats and serialization concepts
- Experience with `encoding/binary` and benchmarking

## Learning Objectives

After completing this challenge, you will be able to:

- **Build** a zero-copy deserialization layer that reads structs directly from byte buffers
- **Design** a wire format that supports direct memory mapping without per-field decoding
- **Handle** alignment, padding, and endianness in a portable binary format
- **Evaluate** the safety and performance tradeoffs of zero-copy vs copying deserialization

## The Challenge

Build a zero-copy message deserialization library. Given a byte buffer containing a serialized message, your library returns a Go struct whose fields point directly into the buffer -- no copying, no allocating, no `encoding/binary.Read`. This is how FlatBuffers, Cap'n Proto, and network packet parsers achieve millions of deserializations per second.

The design works like this: you define a wire format where structs are laid out in memory exactly as Go would lay them out. The "deserialization" is then just a pointer cast: `msg := (*MyMessage)(unsafe.Pointer(&buf[offset]))`. Fields that are fixed-size types (ints, floats, fixed-length arrays) are accessible immediately. Variable-length fields (strings, slices) are stored as offset+length pairs in the fixed part, pointing into a trailing data section of the buffer.

The challenge is doing this correctly. You must handle:

**Alignment**: If `MyMessage` contains an `int64`, the buffer offset must be 8-byte aligned. Unaligned access may be slow (on x86) or crash (on ARM). Your serializer must insert padding to ensure alignment.

**Variable-length data**: Strings and slices cannot be stored inline (they are different sizes across values). Instead, store a `{offset uint32, length uint32}` pair in the fixed portion, and the actual bytes in a variable-length section at the end of the buffer. Your accessor methods resolve these references.

**Safety boundaries**: Every offset must be bounds-checked against the buffer length. A malformed buffer must produce an error, not a segfault.

**Endianness**: For portability, define a canonical byte order (little-endian) and handle conversion on big-endian platforms. For this exercise, assuming little-endian (x86/ARM) is acceptable.

## Requirements

1. Define a binary wire format for fixed-size structs: the first 4 bytes are a magic number, the next 4 bytes are the total message length, followed by the fixed-size fields laid out with proper alignment padding

2. Implement `Serialize(msg *T) ([]byte, error)` that writes a struct into a buffer in wire format -- fixed fields are written at their natural alignment offsets, variable-length fields are written as `{offset, length}` pairs with data appended after the fixed section

3. Implement `Deserialize[T any](buf []byte) (*T, error)` that validates the magic number and length, checks alignment, and returns a pointer into the buffer cast to `*T` -- zero allocation on the deserialization path

4. Support at least these field types: `int32`, `int64`, `uint32`, `uint64`, `float32`, `float64`, `bool`, `[N]byte` (fixed arrays)

5. Implement a `StringRef` type (`struct { Offset uint32; Length uint32 }`) for variable-length strings. Provide an accessor `func (ref StringRef) Resolve(buf []byte) string` that returns a zero-copy string via `unsafe.String` pointing into the buffer

6. Implement a `SliceRef[T]` equivalent for variable-length slices of fixed-size types, with a `Resolve(buf []byte) []T` accessor using `unsafe.Slice`

7. All `Resolve` methods must bounds-check the offset and length against the buffer size, returning empty values for out-of-bounds references

8. Write benchmarks comparing:
   - Your zero-copy deserialization
   - `encoding/binary.Read` into the same struct
   - `encoding/json.Unmarshal` of an equivalent JSON representation
   - Manual field-by-field decoding with `binary.LittleEndian.Uint32` etc.

9. Write a fuzzer (`func FuzzDeserialize(f *testing.F)`) that feeds random byte buffers to your deserializer and verifies it never panics -- only returns errors for invalid input

## Hints

<details>
<summary>Hint 1: Message Layout</summary>

Design the wire format with alignment in mind:

```
Offset  Size  Field
0       4     Magic number (0x4D534730 = "MSG0")
4       4     Total message length
8       ...   Fixed-size fields (aligned)
...     ...   Padding to align
N       ...   Variable-length data section
```

The fixed-size fields mirror the Go struct layout. Use `unsafe.Offsetof` during serialization to place each field at the correct offset.
</details>

<details>
<summary>Hint 2: Zero-Copy Struct Access</summary>

The core deserialization is a single pointer cast:

```go
func Deserialize[T any](buf []byte) (*T, error) {
    if len(buf) < 8 {
        return nil, ErrBufferTooShort
    }
    magic := *(*uint32)(unsafe.Pointer(&buf[0]))
    if magic != MagicNumber {
        return nil, ErrInvalidMagic
    }
    msgLen := *(*uint32)(unsafe.Pointer(&buf[4]))
    if int(msgLen) > len(buf) {
        return nil, ErrBufferTooShort
    }

    headerSize := int(unsafe.Sizeof(*new(T)))
    if len(buf) < 8+headerSize {
        return nil, ErrBufferTooShort
    }

    // Check alignment
    dataPtr := uintptr(unsafe.Pointer(&buf[8]))
    if dataPtr%uintptr(unsafe.Alignof(*new(T))) != 0 {
        return nil, ErrMisaligned
    }

    return (*T)(unsafe.Pointer(&buf[8])), nil
}
```
</details>

<details>
<summary>Hint 3: StringRef Resolution</summary>

```go
type StringRef struct {
    Offset uint32
    Length uint32
}

func (r StringRef) Resolve(buf []byte) string {
    end := int(r.Offset) + int(r.Length)
    if int(r.Offset) > len(buf) || end > len(buf) || end < int(r.Offset) {
        return ""
    }
    return unsafe.String(&buf[r.Offset], int(r.Length))
}
```

The returned string points into `buf` -- no allocation. The string is valid only as long as `buf` is alive and unmodified.
</details>

<details>
<summary>Hint 4: Alignment Padding</summary>

When serializing, pad to the required alignment:

```go
func alignUp(offset, alignment int) int {
    return (offset + alignment - 1) &^ (alignment - 1)
}
```

Before writing a field with alignment N, advance the write position to the next multiple of N.
</details>

<details>
<summary>Hint 5: Fuzzer Structure</summary>

```go
func FuzzDeserialize(f *testing.F) {
    // Seed with a valid message
    validBuf := serialize(MyMessage{...})
    f.Add(validBuf)
    // Seed with empty and minimal buffers
    f.Add([]byte{})
    f.Add([]byte{0x4D, 0x53, 0x47, 0x30})

    f.Fuzz(func(t *testing.T, data []byte) {
        // Must not panic -- errors are fine
        msg, err := Deserialize[MyMessage](data)
        if err != nil {
            return // expected for random input
        }
        // If deserialization succeeded, resolved strings must not panic
        _ = msg.Name.Resolve(data)
    })
}
```
</details>

## Success Criteria

1. Zero-copy deserialization reports 0 `allocs/op` in benchmarks -- the returned struct pointer points directly into the input buffer

2. Round-trip correctness: serialize a struct, deserialize it, verify all fixed fields match and all `StringRef` fields resolve to the original strings

3. Modification through the deserialized pointer is visible in the original buffer (proving zero-copy)

4. Invalid buffers (wrong magic, truncated, misaligned) produce errors, not panics or segfaults

5. The fuzzer runs for 30 seconds with no panics on random input

6. Benchmark shows zero-copy deserialization is at least 10x faster than `encoding/binary.Read` and at least 50x faster than `encoding/json.Unmarshal`

7. StringRef resolution is zero-copy: `unsafe.StringData(resolved)` equals `&buf[ref.Offset]`

8. The library handles edge cases: empty strings (offset=0, length=0), maximum-size messages, and fields at the end of the buffer

## Research Resources

- [FlatBuffers design](https://flatbuffers.dev/flatbuffers_internals.html) -- the canonical zero-copy serialization format
- [Cap'n Proto encoding](https://capnproto.org/encoding.html) -- another zero-copy format with different tradeoffs
- [unsafe.Slice](https://pkg.go.dev/unsafe#Slice) -- creating Go slices from raw pointers
- [unsafe.String](https://pkg.go.dev/unsafe#String) -- creating Go strings from raw pointers
- [Go fuzzing](https://go.dev/doc/security/fuzz/) -- native fuzz testing
- [rkyv (Rust)](https://rkyv.org/) -- zero-copy deserialization in Rust for design inspiration

## What's Next

Zero-copy deserialization is a building block for the final capstone: a memory-mapped data store that combines `mmap`, unsafe pointer access, and zero-copy reads into a complete storage engine.

## Summary

Zero-copy deserialization casts a byte buffer pointer directly to a struct pointer, eliminating allocation and copying. The wire format must match Go's memory layout with proper alignment and padding. Variable-length data (strings, slices) is represented as offset+length references resolved via `unsafe.String` and `unsafe.Slice`. Safety requires bounds-checking all offsets, validating magic numbers and lengths, and fuzz testing against malformed input. The result is 10-100x faster than traditional deserialization at the cost of coupling the wire format to the platform's memory layout and byte order.
