# Exercise 11: Custom Encoding Format

**Difficulty:** Insane | **Estimated Time:** 60 minutes | **Section:** 18 - Encoding

## Challenge

Design and implement a complete custom encoding format called **TLV+ (Type-Length-Value Plus)**. This is a self-describing binary format that can encode arbitrary Go data structures, similar to how `encoding/gob` works but with your own wire format.

## Requirements

### Wire Format

Each encoded value is a TLV triplet:

```
[Type: 1 byte] [Length: 4 bytes big-endian] [Value: Length bytes]
```

Type codes:

| Code | Type | Value encoding |
|------|------|----------------|
| 0x01 | Null | Length=0, no value |
| 0x02 | Bool | Length=1, 0x00=false 0x01=true |
| 0x03 | Int64 | Length=8, big-endian |
| 0x04 | Float64 | Length=8, IEEE 754 big-endian |
| 0x05 | String | Length=N, UTF-8 bytes |
| 0x06 | Bytes | Length=N, raw bytes |
| 0x07 | Array | Length=N, contains concatenated TLV elements preceded by uint32 count |
| 0x08 | Map | Length=N, contains uint32 count followed by alternating key-value TLV pairs |
| 0x09 | Struct | Length=N, contains uint32 field count followed by pairs of (String TLV name + value TLV) |

### Functional Requirements

1. **Encoder**: Write a `TLVEncoder` that accepts any of these Go types and writes TLV+ bytes to an `io.Writer`:
   - `nil`, `bool`, `int`/`int64`, `float64`, `string`, `[]byte`
   - `[]interface{}` (arrays of mixed types)
   - `map[string]interface{}` (string-keyed maps)
   - Structs (via reflection -- encode exported fields)

2. **Decoder**: Write a `TLVDecoder` that reads TLV+ bytes from an `io.Reader` and returns `interface{}` values.

3. **Round-trip correctness**: Encode then decode must produce values equal to the originals.

4. **Nested structures**: The format must handle arbitrarily nested data (maps inside arrays inside structs).

5. **Struct support via reflection**: Use `reflect` to iterate exported struct fields. Encode the field name (as a string TLV) followed by the field value.

### API

```go
type TLVEncoder struct { w io.Writer }
func NewTLVEncoder(w io.Writer) *TLVEncoder
func (e *TLVEncoder) Encode(v interface{}) error

type TLVDecoder struct { r io.Reader }
func NewTLVDecoder(r io.Reader) *TLVDecoder
func (d *TLVDecoder) Decode() (interface{}, error)
```

### Test Data

Your implementation must correctly round-trip this structure:

```go
type Server struct {
    Name    string
    IP      string
    Port    int
    Active  bool
    Tags    []interface{}
    Config  map[string]interface{}
}

server := Server{
    Name:   "prod-web-01",
    IP:     "10.0.1.50",
    Port:   8080,
    Active: true,
    Tags:   []interface{}{"production", "web", int64(3)},
    Config: map[string]interface{}{
        "max_connections": int64(1000),
        "timeout_ms":     int64(5000),
        "debug":          false,
        "cert_path":      "/etc/ssl/cert.pem",
    },
}
```

## Success Criteria

1. All nine type codes encode and decode correctly in isolation
2. The Server struct above round-trips without data loss
3. Nested data (map containing an array containing a map) works
4. Null values are handled
5. A hex dump of the encoded bytes shows the TLV structure clearly (print it)
6. Error handling: decoder returns clear errors for truncated or malformed input
7. Print size comparison: TLV+ bytes vs JSON bytes vs gob bytes for the same data

## Constraints

- No external libraries. Only `encoding/binary`, `math`, `reflect`, `io`, and standard packages.
- No `unsafe` package.
- Struct encoding must use `reflect` to be generic (not hand-coded per struct).

## Guidance

- Start with the simple types (null, bool, int, float, string, bytes). Get those round-tripping first.
- Add arrays and maps second, since they contain recursive TLV values.
- Add struct support last using `reflect.TypeOf` and `reflect.ValueOf`.
- Write a `hexDump` helper that prints encoded bytes in a readable format to debug the wire format.
- For `float64`, use `math.Float64bits` / `math.Float64frombits` to convert to/from `uint64`.

## Stretch Goals

- Add a type code for `time.Time` (0x0A)
- Add optional zlib compression: a wrapper TLV with type 0x0B that contains compressed inner TLV data
- Write a CLI tool that encodes a JSON file to TLV+ and back, comparing sizes
