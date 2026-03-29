# 96. gRPC Framework with Protobuf -- Solution

## Architecture Overview

Both implementations share a four-layer architecture:

1. **Protobuf codec** -- binary encode/decode for the Protocol Buffer wire format
2. **HTTP/2 transport** -- frame encoding/decoding, connection management, stream multiplexing
3. **gRPC framing** -- 5-byte length-prefixed messages on top of HTTP/2 DATA frames
4. **RPC dispatch** -- service registry, handler invocation, error propagation

```
Client                                          Server
  |                                               |
  | -- HTTP/2 HEADERS (:path=/Svc/Method) ------> |
  | -- DATA (gRPC frame: len + protobuf) -------> | -> deserialize -> handler(req)
  | <-- HEADERS (status 200) -------------------- |
  | <-- DATA (gRPC frame: len + protobuf) ------- | <- serialize <- response
  | <-- HEADERS (trailers: grpc-status=0) ------- |
```

## Complete Solution (Go)

### protobuf/varint.go

```go
package protobuf

import (
	"errors"
	"io"
)

var ErrVarintOverflow = errors.New("varint: overflow (more than 10 bytes)")

func EncodeVarint(buf []byte, v uint64) []byte {
	for v >= 0x80 {
		buf = append(buf, byte(v)|0x80)
		v >>= 7
	}
	return append(buf, byte(v))
}

func DecodeVarint(data []byte) (uint64, int, error) {
	var val uint64
	for i := 0; i < len(data) && i < 10; i++ {
		b := data[i]
		val |= uint64(b&0x7F) << (7 * i)
		if b < 0x80 {
			return val, i + 1, nil
		}
	}
	if len(data) == 0 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	return 0, 0, ErrVarintOverflow
}

func ZigzagEncode(v int64) uint64 {
	return uint64((v << 1) ^ (v >> 63))
}

func ZigzagDecode(v uint64) int64 {
	return int64((v >> 1) ^ -(v & 1))
}
```

### protobuf/wire.go

```go
package protobuf

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

const (
	WireVarint          = 0
	Wire64Bit           = 1
	WireLengthDelimited = 2
	Wire32Bit           = 5
)

type Field struct {
	Number   uint32
	WireType uint8
	Data     []byte // raw bytes for the field value
}

func EncodeTag(fieldNum uint32, wireType uint8) uint64 {
	return uint64(fieldNum)<<3 | uint64(wireType)
}

func DecodeTag(tag uint64) (uint32, uint8) {
	return uint32(tag >> 3), uint8(tag & 0x07)
}

type Encoder struct {
	buf []byte
}

func NewEncoder() *Encoder {
	return &Encoder{buf: make([]byte, 0, 256)}
}

func (e *Encoder) Bytes() []byte { return e.buf }
func (e *Encoder) Reset()        { e.buf = e.buf[:0] }

func (e *Encoder) WriteVarintField(fieldNum uint32, val uint64) {
	e.buf = EncodeVarint(e.buf, EncodeTag(fieldNum, WireVarint))
	e.buf = EncodeVarint(e.buf, val)
}

func (e *Encoder) WriteSint64Field(fieldNum uint32, val int64) {
	e.WriteVarintField(fieldNum, ZigzagEncode(val))
}

func (e *Encoder) WriteBoolField(fieldNum uint32, val bool) {
	v := uint64(0)
	if val {
		v = 1
	}
	e.WriteVarintField(fieldNum, v)
}

func (e *Encoder) WriteStringField(fieldNum uint32, val string) {
	e.WriteBytesField(fieldNum, []byte(val))
}

func (e *Encoder) WriteBytesField(fieldNum uint32, val []byte) {
	e.buf = EncodeVarint(e.buf, EncodeTag(fieldNum, WireLengthDelimited))
	e.buf = EncodeVarint(e.buf, uint64(len(val)))
	e.buf = append(e.buf, val...)
}

func (e *Encoder) WriteFixed32Field(fieldNum uint32, val uint32) {
	e.buf = EncodeVarint(e.buf, EncodeTag(fieldNum, Wire32Bit))
	e.buf = binary.LittleEndian.AppendUint32(e.buf, val)
}

func (e *Encoder) WriteFloatField(fieldNum uint32, val float32) {
	e.WriteFixed32Field(fieldNum, math.Float32bits(val))
}

func (e *Encoder) WriteFixed64Field(fieldNum uint32, val uint64) {
	e.buf = EncodeVarint(e.buf, EncodeTag(fieldNum, Wire64Bit))
	e.buf = binary.LittleEndian.AppendUint64(e.buf, val)
}

func (e *Encoder) WriteDoubleField(fieldNum uint32, val float64) {
	e.WriteFixed64Field(fieldNum, math.Float64bits(val))
}

func (e *Encoder) WriteMessageField(fieldNum uint32, msg []byte) {
	e.WriteBytesField(fieldNum, msg)
}

func (e *Encoder) WritePackedVarints(fieldNum uint32, vals []uint64) {
	inner := NewEncoder()
	for _, v := range vals {
		inner.buf = EncodeVarint(inner.buf, v)
	}
	e.WriteBytesField(fieldNum, inner.Bytes())
}

type Decoder struct {
	data []byte
	pos  int
}

func NewDecoder(data []byte) *Decoder {
	return &Decoder{data: data}
}

func (d *Decoder) Remaining() int { return len(d.data) - d.pos }

func (d *Decoder) NextField() (Field, error) {
	if d.pos >= len(d.data) {
		return Field{}, io.EOF
	}

	tag, n, err := DecodeVarint(d.data[d.pos:])
	if err != nil {
		return Field{}, err
	}
	d.pos += n

	fieldNum, wireType := DecodeTag(tag)
	f := Field{Number: fieldNum, WireType: wireType}

	switch wireType {
	case WireVarint:
		_, vn, err := DecodeVarint(d.data[d.pos:])
		if err != nil {
			return Field{}, err
		}
		f.Data = d.data[d.pos : d.pos+vn]
		d.pos += vn

	case Wire64Bit:
		if d.pos+8 > len(d.data) {
			return Field{}, io.ErrUnexpectedEOF
		}
		f.Data = d.data[d.pos : d.pos+8]
		d.pos += 8

	case WireLengthDelimited:
		length, ln, err := DecodeVarint(d.data[d.pos:])
		if err != nil {
			return Field{}, err
		}
		d.pos += ln
		end := d.pos + int(length)
		if end > len(d.data) {
			return Field{}, io.ErrUnexpectedEOF
		}
		f.Data = d.data[d.pos:end]
		d.pos = end

	case Wire32Bit:
		if d.pos+4 > len(d.data) {
			return Field{}, io.ErrUnexpectedEOF
		}
		f.Data = d.data[d.pos : d.pos+4]
		d.pos += 4

	default:
		return Field{}, errors.New("unknown wire type")
	}

	return f, nil
}

func FieldToVarint(f Field) (uint64, error) {
	v, _, err := DecodeVarint(f.Data)
	return v, err
}

func FieldToString(f Field) string {
	return string(f.Data)
}

func FieldToFloat32(f Field) float32 {
	bits := binary.LittleEndian.Uint32(f.Data)
	return math.Float32frombits(bits)
}

func FieldToFloat64(f Field) float64 {
	bits := binary.LittleEndian.Uint64(f.Data)
	return math.Float64frombits(bits)
}
```

### http2/frames.go

```go
package http2

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	FrameData         = 0x0
	FrameHeaders      = 0x1
	FrameSettings     = 0x4
	FrameRSTStream    = 0x3
	FrameGoaway       = 0x7
	FrameWindowUpdate = 0x8

	FlagEndStream  = 0x1
	FlagEndHeaders = 0x4
	FlagAck        = 0x1
)

var ConnectionPreface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")

type Frame struct {
	Type     uint8
	Flags    uint8
	StreamID uint32
	Payload  []byte
}

func EncodeFrame(f Frame) []byte {
	length := len(f.Payload)
	header := make([]byte, 9+length)
	header[0] = byte(length >> 16)
	header[1] = byte(length >> 8)
	header[2] = byte(length)
	header[3] = f.Type
	header[4] = f.Flags
	binary.BigEndian.PutUint32(header[5:9], f.StreamID&0x7FFFFFFF)
	copy(header[9:], f.Payload)
	return header
}

func DecodeFrame(r io.Reader) (Frame, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}

	length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
	f := Frame{
		Type:     header[3],
		Flags:    header[4],
		StreamID: binary.BigEndian.Uint32(header[5:9]) & 0x7FFFFFFF,
	}

	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, err
		}
	}

	return f, nil
}

func EncodeSettingsFrame(ack bool) []byte {
	flags := uint8(0)
	if ack {
		flags = FlagAck
	}
	return EncodeFrame(Frame{Type: FrameSettings, Flags: flags, StreamID: 0})
}

func EncodeRSTStream(streamID uint32, errorCode uint32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, errorCode)
	return EncodeFrame(Frame{Type: FrameRSTStream, StreamID: streamID, Payload: payload})
}

// Simplified HPACK: static table only, no Huffman, no dynamic table.
// Encodes headers as literal-without-indexing.
func EncodeHeaders(headers map[string]string) []byte {
	var buf []byte
	for k, v := range headers {
		buf = append(buf, 0x00) // literal without indexing, new name
		buf = append(buf, encodeHpackString(k)...)
		buf = append(buf, encodeHpackString(v)...)
	}
	return buf
}

func encodeHpackString(s string) []byte {
	length := len(s)
	buf := make([]byte, 0, 1+length)
	buf = append(buf, byte(length)) // no Huffman flag, 7-bit length
	buf = append(buf, []byte(s)...)
	return buf
}

func DecodeHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	pos := 0
	for pos < len(data) {
		if data[pos] != 0x00 {
			return nil, errors.New("only literal-without-indexing supported")
		}
		pos++
		name, n := decodeHpackString(data[pos:])
		pos += n
		value, n := decodeHpackString(data[pos:])
		pos += n
		headers[name] = value
	}
	return headers, nil
}

func decodeHpackString(data []byte) (string, int) {
	length := int(data[0] & 0x7F)
	return string(data[1 : 1+length]), 1 + length
}
```

### grpc/framing.go

```go
package grpc

import "encoding/binary"

func EncodeGRPCMessage(msg []byte) []byte {
	frame := make([]byte, 5+len(msg))
	frame[0] = 0 // not compressed
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(msg)))
	copy(frame[5:], msg)
	return frame
}

func DecodeGRPCMessage(data []byte) ([]byte, bool, error) {
	if len(data) < 5 {
		return nil, false, ErrIncompleteFrame
	}
	compressed := data[0] == 1
	length := binary.BigEndian.Uint32(data[1:5])
	if uint32(len(data)-5) < length {
		return nil, false, ErrIncompleteFrame
	}
	return data[5 : 5+length], compressed, nil
}
```

### grpc/server.go

```go
package grpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"grpcfw/http2"
)

var (
	ErrIncompleteFrame = errors.New("grpc: incomplete message frame")
	ErrNotFound        = errors.New("grpc: method not found")
)

type StatusCode int

const (
	StatusOK               StatusCode = 0
	StatusCancelled        StatusCode = 1
	StatusUnknown          StatusCode = 2
	StatusInvalidArgument  StatusCode = 3
	StatusNotFound         StatusCode = 5
	StatusInternal         StatusCode = 13
	StatusUnavailable      StatusCode = 14
	StatusDeadlineExceeded StatusCode = 4
)

type UnaryHandler func(ctx context.Context, req []byte) ([]byte, error)
type StreamHandler func(ctx context.Context, req []byte, stream ResponseStream) error

type ResponseStream interface {
	Send(msg []byte) error
}

type Server struct {
	mu             sync.RWMutex
	unaryHandlers  map[string]UnaryHandler
	streamHandlers map[string]StreamHandler
	listener       net.Listener
}

func NewServer() *Server {
	return &Server{
		unaryHandlers:  make(map[string]UnaryHandler),
		streamHandlers: make(map[string]StreamHandler),
	}
}

func (s *Server) RegisterUnary(path string, handler UnaryHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unaryHandlers[path] = handler
}

func (s *Server) RegisterStream(path string, handler StreamHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamHandlers[path] = handler
}

func (s *Server) Serve(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read client connection preface
	preface := make([]byte, len(http2.ConnectionPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return
	}

	// Send server settings
	conn.Write(http2.EncodeSettingsFrame(false))

	// Read client settings
	frame, err := http2.DecodeFrame(conn)
	if err != nil {
		return
	}
	if frame.Type == http2.FrameSettings {
		conn.Write(http2.EncodeSettingsFrame(true)) // ACK
	}

	// Process frames
	for {
		frame, err := http2.DecodeFrame(conn)
		if err != nil {
			return
		}
		s.handleFrame(conn, frame)
	}
}

func (s *Server) handleFrame(conn net.Conn, frame http2.Frame) {
	switch frame.Type {
	case http2.FrameHeaders:
		headers, err := http2.DecodeHeaders(frame.Payload)
		if err != nil {
			return
		}
		path := headers[":path"]
		timeout := headers["grpc-timeout"]

		ctx := context.Background()
		if timeout != "" {
			if d, err := parseTimeout(timeout); err == nil {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(d))
				defer cancel()
			}
		}

		// Read DATA frame with request
		dataFrame, err := http2.DecodeFrame(conn)
		if err != nil || dataFrame.Type != http2.FrameData {
			return
		}
		reqBytes, _, err := DecodeGRPCMessage(dataFrame.Payload)
		if err != nil {
			return
		}

		s.dispatch(ctx, conn, frame.StreamID, path, reqBytes)

	case http2.FrameSettings:
		if frame.Flags&http2.FlagAck == 0 {
			conn.Write(http2.EncodeSettingsFrame(true))
		}

	case http2.FrameWindowUpdate:
		// Accept but do not enforce flow control

	default:
		// Ignore unknown frames
	}
}

func (s *Server) dispatch(ctx context.Context, conn net.Conn, streamID uint32, path string, req []byte) {
	s.mu.RLock()
	unary, hasUnary := s.unaryHandlers[path]
	stream, hasStream := s.streamHandlers[path]
	s.mu.RUnlock()

	if hasUnary {
		resp, err := unary(ctx, req)
		status := StatusOK
		msg := ""
		if err != nil {
			status = StatusInternal
			msg = err.Error()
		}
		sendResponse(conn, streamID, resp)
		sendTrailers(conn, streamID, status, msg)
	} else if hasStream {
		rs := &responseStreamImpl{conn: conn, streamID: streamID}
		sendResponseHeaders(conn, streamID)
		err := stream(ctx, req, rs)
		status := StatusOK
		msg := ""
		if err != nil {
			status = StatusInternal
			msg = err.Error()
		}
		sendTrailers(conn, streamID, status, msg)
	} else {
		sendTrailers(conn, streamID, StatusNotFound, "method not found: "+path)
	}
}

type responseStreamImpl struct {
	conn     net.Conn
	streamID uint32
}

func (rs *responseStreamImpl) Send(msg []byte) error {
	frame := http2.Frame{
		Type:     http2.FrameData,
		StreamID: rs.streamID,
		Payload:  EncodeGRPCMessage(msg),
	}
	_, err := rs.conn.Write(http2.EncodeFrame(frame))
	return err
}

func sendResponseHeaders(conn net.Conn, streamID uint32) {
	headers := http2.EncodeHeaders(map[string]string{
		":status":      "200",
		"content-type": "application/grpc",
	})
	frame := http2.Frame{
		Type:     http2.FrameHeaders,
		Flags:    http2.FlagEndHeaders,
		StreamID: streamID,
		Payload:  headers,
	}
	conn.Write(http2.EncodeFrame(frame))
}

func sendResponse(conn net.Conn, streamID uint32, resp []byte) {
	sendResponseHeaders(conn, streamID)
	if resp != nil {
		data := http2.Frame{
			Type:     http2.FrameData,
			StreamID: streamID,
			Payload:  EncodeGRPCMessage(resp),
		}
		conn.Write(http2.EncodeFrame(data))
	}
}

func sendTrailers(conn net.Conn, streamID uint32, status StatusCode, msg string) {
	trailers := map[string]string{
		"grpc-status": fmt.Sprintf("%d", status),
	}
	if msg != "" {
		trailers["grpc-message"] = msg
	}
	frame := http2.Frame{
		Type:     http2.FrameHeaders,
		Flags:    http2.FlagEndStream | http2.FlagEndHeaders,
		StreamID: streamID,
		Payload:  http2.EncodeHeaders(trailers),
	}
	conn.Write(http2.EncodeFrame(frame))
}

func parseTimeout(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid timeout: %s", s)
	}
	unit := s[len(s)-1:]
	numStr := s[:len(s)-1]
	var num int
	fmt.Sscanf(numStr, "%d", &num)

	switch strings.ToUpper(unit) {
	case "H":
		return time.Duration(num) * time.Hour, nil
	case "M":
		return time.Duration(num) * time.Minute, nil
	case "S":
		return time.Duration(num) * time.Second, nil
	case "U":
		return time.Duration(num) * time.Microsecond, nil
	case "N":
		return time.Duration(num) * time.Nanosecond, nil
	default:
		return time.Duration(num) * time.Millisecond, nil
	}
}
```

### grpc/client.go

```go
package grpc

import (
	"fmt"
	"io"
	"net"

	"grpcfw/http2"
)

type Client struct {
	conn     net.Conn
	streamID uint32
}

func Dial(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	// Send connection preface
	conn.Write(http2.ConnectionPreface)
	conn.Write(http2.EncodeSettingsFrame(false))

	// Read server settings
	frame, err := http2.DecodeFrame(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if frame.Type == http2.FrameSettings {
		conn.Write(http2.EncodeSettingsFrame(true))
	}

	return &Client{conn: conn, streamID: 1}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) CallUnary(path string, req []byte, metadata map[string]string) ([]byte, StatusCode, error) {
	streamID := c.nextStreamID()

	headers := map[string]string{
		":method":      "POST",
		":path":        path,
		"content-type": "application/grpc",
	}
	for k, v := range metadata {
		headers[k] = v
	}

	headerFrame := http2.Frame{
		Type:     http2.FrameHeaders,
		Flags:    http2.FlagEndHeaders,
		StreamID: streamID,
		Payload:  http2.EncodeHeaders(headers),
	}
	c.conn.Write(http2.EncodeFrame(headerFrame))

	dataFrame := http2.Frame{
		Type:     http2.FrameData,
		Flags:    http2.FlagEndStream,
		StreamID: streamID,
		Payload:  EncodeGRPCMessage(req),
	}
	c.conn.Write(http2.EncodeFrame(dataFrame))

	// Read response
	var respData []byte
	status := StatusOK

	for {
		frame, err := http2.DecodeFrame(c.conn)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, StatusInternal, err
		}

		switch frame.Type {
		case http2.FrameHeaders:
			if frame.Flags&http2.FlagEndStream != 0 {
				trailers, _ := http2.DecodeHeaders(frame.Payload)
				if s, ok := trailers["grpc-status"]; ok {
					fmt.Sscanf(s, "%d", &status)
				}
				return respData, status, nil
			}
		case http2.FrameData:
			msg, _, err := DecodeGRPCMessage(frame.Payload)
			if err != nil {
				return nil, StatusInternal, err
			}
			respData = msg
		}
	}

	return respData, status, nil
}

func (c *Client) CallServerStream(path string, req []byte) ([][]byte, StatusCode, error) {
	streamID := c.nextStreamID()

	headers := map[string]string{
		":method":      "POST",
		":path":        path,
		"content-type": "application/grpc",
	}

	headerFrame := http2.Frame{
		Type:     http2.FrameHeaders,
		Flags:    http2.FlagEndHeaders,
		StreamID: streamID,
		Payload:  http2.EncodeHeaders(headers),
	}
	c.conn.Write(http2.EncodeFrame(headerFrame))

	dataFrame := http2.Frame{
		Type:     http2.FrameData,
		Flags:    http2.FlagEndStream,
		StreamID: streamID,
		Payload:  EncodeGRPCMessage(req),
	}
	c.conn.Write(http2.EncodeFrame(dataFrame))

	var messages [][]byte
	status := StatusOK

	for {
		frame, err := http2.DecodeFrame(c.conn)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, StatusInternal, err
		}

		switch frame.Type {
		case http2.FrameHeaders:
			if frame.Flags&http2.FlagEndStream != 0 {
				trailers, _ := http2.DecodeHeaders(frame.Payload)
				if s, ok := trailers["grpc-status"]; ok {
					fmt.Sscanf(s, "%d", &status)
				}
				return messages, status, nil
			}
		case http2.FrameData:
			msg, _, err := DecodeGRPCMessage(frame.Payload)
			if err != nil {
				return nil, StatusInternal, err
			}
			messages = append(messages, msg)
		}
	}

	return messages, status, nil
}

func (c *Client) nextStreamID() uint32 {
	id := c.streamID
	c.streamID += 2 // client streams are odd
	return id
}
```

## Complete Solution (Rust)

### src/protobuf.rs

```rust
use std::io::{self, Read, Write};

pub fn encode_varint(buf: &mut Vec<u8>, mut val: u64) {
    loop {
        let byte = (val & 0x7F) as u8;
        val >>= 7;
        if val == 0 {
            buf.push(byte);
            return;
        }
        buf.push(byte | 0x80);
    }
}

pub fn decode_varint(data: &[u8]) -> Result<(u64, usize), io::Error> {
    let mut val: u64 = 0;
    for (i, &byte) in data.iter().enumerate().take(10) {
        val |= ((byte & 0x7F) as u64) << (7 * i);
        if byte < 0x80 {
            return Ok((val, i + 1));
        }
    }
    Err(io::Error::new(io::ErrorKind::InvalidData, "varint overflow"))
}

pub fn zigzag_encode(v: i64) -> u64 {
    ((v << 1) ^ (v >> 63)) as u64
}

pub fn zigzag_decode(v: u64) -> i64 {
    ((v >> 1) as i64) ^ -((v & 1) as i64)
}

pub const WIRE_VARINT: u8 = 0;
pub const WIRE_64BIT: u8 = 1;
pub const WIRE_LENGTH_DELIMITED: u8 = 2;
pub const WIRE_32BIT: u8 = 5;

pub fn encode_tag(field_num: u32, wire_type: u8) -> u64 {
    ((field_num as u64) << 3) | (wire_type as u64)
}

pub fn decode_tag(tag: u64) -> (u32, u8) {
    ((tag >> 3) as u32, (tag & 0x07) as u8)
}

pub struct Encoder {
    buf: Vec<u8>,
}

impl Encoder {
    pub fn new() -> Self {
        Encoder { buf: Vec::with_capacity(256) }
    }

    pub fn into_bytes(self) -> Vec<u8> { self.buf }
    pub fn as_bytes(&self) -> &[u8] { &self.buf }

    pub fn write_varint_field(&mut self, field_num: u32, val: u64) {
        encode_varint(&mut self.buf, encode_tag(field_num, WIRE_VARINT));
        encode_varint(&mut self.buf, val);
    }

    pub fn write_sint64_field(&mut self, field_num: u32, val: i64) {
        self.write_varint_field(field_num, zigzag_encode(val));
    }

    pub fn write_string_field(&mut self, field_num: u32, val: &str) {
        self.write_bytes_field(field_num, val.as_bytes());
    }

    pub fn write_bytes_field(&mut self, field_num: u32, val: &[u8]) {
        encode_varint(&mut self.buf, encode_tag(field_num, WIRE_LENGTH_DELIMITED));
        encode_varint(&mut self.buf, val.len() as u64);
        self.buf.extend_from_slice(val);
    }

    pub fn write_float_field(&mut self, field_num: u32, val: f32) {
        encode_varint(&mut self.buf, encode_tag(field_num, WIRE_32BIT));
        self.buf.extend_from_slice(&val.to_le_bytes());
    }

    pub fn write_double_field(&mut self, field_num: u32, val: f64) {
        encode_varint(&mut self.buf, encode_tag(field_num, WIRE_64BIT));
        self.buf.extend_from_slice(&val.to_le_bytes());
    }

    pub fn write_packed_varints(&mut self, field_num: u32, vals: &[u64]) {
        let mut inner = Vec::new();
        for &v in vals {
            encode_varint(&mut inner, v);
        }
        self.write_bytes_field(field_num, &inner);
    }
}

pub struct Field {
    pub number: u32,
    pub wire_type: u8,
    pub data: Vec<u8>,
}

pub struct Decoder<'a> {
    data: &'a [u8],
    pos: usize,
}

impl<'a> Decoder<'a> {
    pub fn new(data: &'a [u8]) -> Self {
        Decoder { data, pos: 0 }
    }

    pub fn next_field(&mut self) -> Result<Option<Field>, io::Error> {
        if self.pos >= self.data.len() {
            return Ok(None);
        }

        let (tag, n) = decode_varint(&self.data[self.pos..])?;
        self.pos += n;
        let (field_num, wire_type) = decode_tag(tag);

        let data = match wire_type {
            WIRE_VARINT => {
                let (_, vn) = decode_varint(&self.data[self.pos..])?;
                let d = self.data[self.pos..self.pos + vn].to_vec();
                self.pos += vn;
                d
            }
            WIRE_64BIT => {
                let d = self.data[self.pos..self.pos + 8].to_vec();
                self.pos += 8;
                d
            }
            WIRE_LENGTH_DELIMITED => {
                let (length, ln) = decode_varint(&self.data[self.pos..])?;
                self.pos += ln;
                let end = self.pos + length as usize;
                let d = self.data[self.pos..end].to_vec();
                self.pos = end;
                d
            }
            WIRE_32BIT => {
                let d = self.data[self.pos..self.pos + 4].to_vec();
                self.pos += 4;
                d
            }
            _ => return Err(io::Error::new(io::ErrorKind::InvalidData, "unknown wire type")),
        };

        Ok(Some(Field { number: field_num, wire_type, data }))
    }
}

pub fn field_to_varint(f: &Field) -> Result<u64, io::Error> {
    let (v, _) = decode_varint(&f.data)?;
    Ok(v)
}

pub fn field_to_string(f: &Field) -> String {
    String::from_utf8_lossy(&f.data).into_owned()
}
```

### src/http2.rs

```rust
use std::collections::HashMap;
use std::io::{self, Read, Write};

pub const FRAME_DATA: u8 = 0x0;
pub const FRAME_HEADERS: u8 = 0x1;
pub const FRAME_SETTINGS: u8 = 0x4;
pub const FRAME_RST_STREAM: u8 = 0x3;
pub const FRAME_WINDOW_UPDATE: u8 = 0x8;

pub const FLAG_END_STREAM: u8 = 0x1;
pub const FLAG_END_HEADERS: u8 = 0x4;
pub const FLAG_ACK: u8 = 0x1;

pub const CONNECTION_PREFACE: &[u8] = b"PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n";

pub struct Frame {
    pub frame_type: u8,
    pub flags: u8,
    pub stream_id: u32,
    pub payload: Vec<u8>,
}

pub fn encode_frame(f: &Frame) -> Vec<u8> {
    let length = f.payload.len();
    let mut buf = Vec::with_capacity(9 + length);
    buf.push((length >> 16) as u8);
    buf.push((length >> 8) as u8);
    buf.push(length as u8);
    buf.push(f.frame_type);
    buf.push(f.flags);
    buf.extend_from_slice(&(f.stream_id & 0x7FFFFFFF).to_be_bytes());
    buf.extend_from_slice(&f.payload);
    buf
}

pub fn decode_frame<R: Read>(reader: &mut R) -> io::Result<Frame> {
    let mut header = [0u8; 9];
    reader.read_exact(&mut header)?;

    let length = ((header[0] as usize) << 16)
        | ((header[1] as usize) << 8)
        | (header[2] as usize);

    let mut payload = vec![0u8; length];
    if length > 0 {
        reader.read_exact(&mut payload)?;
    }

    Ok(Frame {
        frame_type: header[3],
        flags: header[4],
        stream_id: u32::from_be_bytes([header[5], header[6], header[7], header[8]])
            & 0x7FFFFFFF,
        payload,
    })
}

pub fn encode_headers(headers: &HashMap<String, String>) -> Vec<u8> {
    let mut buf = Vec::new();
    for (k, v) in headers {
        buf.push(0x00); // literal without indexing
        encode_hpack_string(&mut buf, k);
        encode_hpack_string(&mut buf, v);
    }
    buf
}

fn encode_hpack_string(buf: &mut Vec<u8>, s: &str) {
    buf.push(s.len() as u8);
    buf.extend_from_slice(s.as_bytes());
}

pub fn decode_headers(data: &[u8]) -> io::Result<HashMap<String, String>> {
    let mut headers = HashMap::new();
    let mut pos = 0;
    while pos < data.len() {
        if data[pos] != 0x00 {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "only literal-without-indexing supported",
            ));
        }
        pos += 1;
        let (name, n) = decode_hpack_string(&data[pos..]);
        pos += n;
        let (value, n) = decode_hpack_string(&data[pos..]);
        pos += n;
        headers.insert(name, value);
    }
    Ok(headers)
}

fn decode_hpack_string(data: &[u8]) -> (String, usize) {
    let length = (data[0] & 0x7F) as usize;
    let s = String::from_utf8_lossy(&data[1..1 + length]).into_owned();
    (s, 1 + length)
}
```

### src/grpc.rs

```rust
use std::collections::HashMap;
use std::io::{self, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::{Arc, RwLock};

use crate::http2;

pub fn encode_grpc_message(msg: &[u8]) -> Vec<u8> {
    let mut frame = Vec::with_capacity(5 + msg.len());
    frame.push(0); // not compressed
    frame.extend_from_slice(&(msg.len() as u32).to_be_bytes());
    frame.extend_from_slice(msg);
    frame
}

pub fn decode_grpc_message(data: &[u8]) -> io::Result<(&[u8], bool)> {
    if data.len() < 5 {
        return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "incomplete frame"));
    }
    let compressed = data[0] == 1;
    let length = u32::from_be_bytes([data[1], data[2], data[3], data[4]]) as usize;
    if data.len() - 5 < length {
        return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "incomplete payload"));
    }
    Ok((&data[5..5 + length], compressed))
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum StatusCode {
    Ok = 0,
    Cancelled = 1,
    Unknown = 2,
    InvalidArgument = 3,
    DeadlineExceeded = 4,
    NotFound = 5,
    Internal = 13,
    Unavailable = 14,
}

pub type UnaryHandler = Arc<dyn Fn(&[u8]) -> Result<Vec<u8>, String> + Send + Sync>;
pub type StreamHandler = Arc<dyn Fn(&[u8], &mut dyn FnMut(&[u8])) -> Result<(), String> + Send + Sync>;

pub struct Server {
    unary_handlers: Arc<RwLock<HashMap<String, UnaryHandler>>>,
    stream_handlers: Arc<RwLock<HashMap<String, StreamHandler>>>,
}

impl Server {
    pub fn new() -> Self {
        Server {
            unary_handlers: Arc::new(RwLock::new(HashMap::new())),
            stream_handlers: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub fn register_unary<F>(&self, path: &str, handler: F)
    where
        F: Fn(&[u8]) -> Result<Vec<u8>, String> + Send + Sync + 'static,
    {
        self.unary_handlers
            .write()
            .unwrap()
            .insert(path.to_string(), Arc::new(handler));
    }

    pub fn register_stream<F>(&self, path: &str, handler: F)
    where
        F: Fn(&[u8], &mut dyn FnMut(&[u8])) -> Result<(), String> + Send + Sync + 'static,
    {
        self.stream_handlers
            .write()
            .unwrap()
            .insert(path.to_string(), Arc::new(handler));
    }

    pub fn serve(&self, addr: &str) -> io::Result<()> {
        let listener = TcpListener::bind(addr)?;
        for stream in listener.incoming() {
            let stream = stream?;
            let unary = Arc::clone(&self.unary_handlers);
            let streaming = Arc::clone(&self.stream_handlers);
            std::thread::spawn(move || {
                handle_connection(stream, &unary, &streaming);
            });
        }
        Ok(())
    }
}

fn handle_connection(
    mut stream: TcpStream,
    unary: &RwLock<HashMap<String, UnaryHandler>>,
    streaming: &RwLock<HashMap<String, StreamHandler>>,
) {
    // Read client preface
    let mut preface = vec![0u8; http2::CONNECTION_PREFACE.len()];
    if stream.read_exact(&mut preface).is_err() {
        return;
    }

    // Send settings
    let settings = http2::encode_frame(&http2::Frame {
        frame_type: http2::FRAME_SETTINGS,
        flags: 0,
        stream_id: 0,
        payload: vec![],
    });
    let _ = stream.write_all(&settings);

    // Read client settings and ACK
    if let Ok(frame) = http2::decode_frame(&mut stream) {
        if frame.frame_type == http2::FRAME_SETTINGS {
            let ack = http2::encode_frame(&http2::Frame {
                frame_type: http2::FRAME_SETTINGS,
                flags: http2::FLAG_ACK,
                stream_id: 0,
                payload: vec![],
            });
            let _ = stream.write_all(&ack);
        }
    }

    loop {
        let frame = match http2::decode_frame(&mut stream) {
            Ok(f) => f,
            Err(_) => return,
        };

        if frame.frame_type == http2::FRAME_HEADERS {
            let headers = match http2::decode_headers(&frame.payload) {
                Ok(h) => h,
                Err(_) => continue,
            };

            let path = headers.get(":path").cloned().unwrap_or_default();
            let stream_id = frame.stream_id;

            // Read DATA frame
            let data_frame = match http2::decode_frame(&mut stream) {
                Ok(f) => f,
                Err(_) => return,
            };
            let (req_bytes, _) = match decode_grpc_message(&data_frame.payload) {
                Ok(r) => r,
                Err(_) => continue,
            };

            // Dispatch
            let unary_map = unary.read().unwrap();
            let stream_map = streaming.read().unwrap();

            if let Some(handler) = unary_map.get(&path) {
                match handler(req_bytes) {
                    Ok(resp) => {
                        send_response(&mut stream, stream_id, &resp);
                        send_trailers(&mut stream, stream_id, StatusCode::Ok, "");
                    }
                    Err(msg) => {
                        send_trailers(&mut stream, stream_id, StatusCode::Internal, &msg);
                    }
                }
            } else if let Some(handler) = stream_map.get(&path) {
                send_response_headers(&mut stream, stream_id);
                let stream_clone = stream.try_clone().unwrap();
                let mut sender = move |msg: &[u8]| {
                    // This closure is used by handlers to send stream messages
                    let _ = send_data_frame(&stream_clone, stream_id, msg);
                };
                match handler(req_bytes, &mut sender) {
                    Ok(()) => send_trailers(&mut stream, stream_id, StatusCode::Ok, ""),
                    Err(msg) => send_trailers(&mut stream, stream_id, StatusCode::Internal, &msg),
                }
            } else {
                send_trailers(&mut stream, stream_id, StatusCode::NotFound, "method not found");
            }
        }
    }
}

fn send_response_headers<W: Write>(w: &mut W, stream_id: u32) {
    let mut headers = HashMap::new();
    headers.insert(":status".to_string(), "200".to_string());
    headers.insert("content-type".to_string(), "application/grpc".to_string());

    let frame = http2::encode_frame(&http2::Frame {
        frame_type: http2::FRAME_HEADERS,
        flags: http2::FLAG_END_HEADERS,
        stream_id,
        payload: http2::encode_headers(&headers),
    });
    let _ = w.write_all(&frame);
}

fn send_response<W: Write>(w: &mut W, stream_id: u32, resp: &[u8]) {
    send_response_headers(w, stream_id);
    let _ = send_data_frame(w, stream_id, resp);
}

fn send_data_frame<W: Write>(w: &W, stream_id: u32, msg: &[u8]) -> io::Result<()> {
    let frame = http2::encode_frame(&http2::Frame {
        frame_type: http2::FRAME_DATA,
        stream_id,
        flags: 0,
        payload: encode_grpc_message(msg),
    });
    let mut w = w;
    w.write_all(&frame)
}

fn send_trailers<W: Write>(w: &mut W, stream_id: u32, status: StatusCode, msg: &str) {
    let mut trailers = HashMap::new();
    trailers.insert("grpc-status".to_string(), (status as u32).to_string());
    if !msg.is_empty() {
        trailers.insert("grpc-message".to_string(), msg.to_string());
    }

    let frame = http2::encode_frame(&http2::Frame {
        frame_type: http2::FRAME_HEADERS,
        flags: http2::FLAG_END_STREAM | http2::FLAG_END_HEADERS,
        stream_id,
        payload: http2::encode_headers(&trailers),
    });
    let _ = w.write_all(&frame);
}
```

## Tests

### Go tests (protobuf_test.go)

```go
package protobuf

import "testing"

func TestVarintRoundTrip(t *testing.T) {
	values := []uint64{0, 1, 127, 128, 150, 300, 16384, 1<<63 - 1}
	for _, v := range values {
		buf := EncodeVarint(nil, v)
		decoded, n, err := DecodeVarint(buf)
		if err != nil {
			t.Fatalf("DecodeVarint(%d): %v", v, err)
		}
		if decoded != v || n != len(buf) {
			t.Errorf("varint round-trip: %d -> %d (bytes: %d/%d)", v, decoded, n, len(buf))
		}
	}
}

func TestVarintKnownEncodings(t *testing.T) {
	// 150 -> [0x96, 0x01] per protobuf spec
	buf := EncodeVarint(nil, 150)
	if len(buf) != 2 || buf[0] != 0x96 || buf[1] != 0x01 {
		t.Errorf("150 encoded as %x, want [96 01]", buf)
	}
}

func TestZigzag(t *testing.T) {
	pairs := [][2]int64{{0, 0}, {-1, 1}, {1, 2}, {-2, 3}, {2, 4}}
	for _, p := range pairs {
		encoded := ZigzagEncode(p[0])
		if int64(encoded) != p[1] {
			t.Errorf("ZigzagEncode(%d) = %d, want %d", p[0], encoded, p[1])
		}
		decoded := ZigzagDecode(encoded)
		if decoded != p[0] {
			t.Errorf("ZigzagDecode(%d) = %d, want %d", encoded, decoded, p[0])
		}
	}
}

func TestMessageRoundTrip(t *testing.T) {
	enc := NewEncoder()
	enc.WriteStringField(1, "hello")
	enc.WriteVarintField(2, 42)
	enc.WriteBoolField(3, true)
	enc.WriteDoubleField(4, 3.14)

	dec := NewDecoder(enc.Bytes())
	f, _ := dec.NextField()
	if FieldToString(f) != "hello" {
		t.Error("field 1 mismatch")
	}
	f, _ = dec.NextField()
	v, _ := FieldToVarint(f)
	if v != 42 {
		t.Error("field 2 mismatch")
	}
	f, _ = dec.NextField()
	bv, _ := FieldToVarint(f)
	if bv != 1 {
		t.Error("field 3 mismatch")
	}
}

func TestPackedRepeated(t *testing.T) {
	enc := NewEncoder()
	enc.WritePackedVarints(1, []uint64{1, 2, 3, 4, 5})

	dec := NewDecoder(enc.Bytes())
	f, _ := dec.NextField()
	if f.WireType != WireLengthDelimited {
		t.Error("packed field should be length-delimited")
	}
}
```

### Rust tests

```rust
#[cfg(test)]
mod tests {
    use super::protobuf::*;

    #[test]
    fn varint_round_trip() {
        let values = vec![0u64, 1, 127, 128, 150, 300, 16384, (1u64 << 63) - 1];
        for v in values {
            let mut buf = Vec::new();
            encode_varint(&mut buf, v);
            let (decoded, _) = decode_varint(&buf).unwrap();
            assert_eq!(decoded, v, "round-trip failed for {v}");
        }
    }

    #[test]
    fn varint_known_encoding() {
        let mut buf = Vec::new();
        encode_varint(&mut buf, 150);
        assert_eq!(buf, vec![0x96, 0x01]);
    }

    #[test]
    fn zigzag_round_trip() {
        let cases = vec![(0i64, 0u64), (-1, 1), (1, 2), (-2, 3), (2, 4)];
        for (signed, unsigned) in cases {
            assert_eq!(zigzag_encode(signed), unsigned);
            assert_eq!(zigzag_decode(unsigned), signed);
        }
    }

    #[test]
    fn message_round_trip() {
        let mut enc = Encoder::new();
        enc.write_string_field(1, "hello");
        enc.write_varint_field(2, 42);

        let mut dec = Decoder::new(enc.as_bytes());
        let f1 = dec.next_field().unwrap().unwrap();
        assert_eq!(field_to_string(&f1), "hello");

        let f2 = dec.next_field().unwrap().unwrap();
        assert_eq!(field_to_varint(&f2).unwrap(), 42);
    }
}
```

## Running the Solutions

```bash
# Go
cd go && go mod init grpcfw
go test ./protobuf/... ./http2/... ./grpc/...

# Rust
cd rust && cargo init --name grpcfw
cargo test
```

## Expected Output

```
# Go
=== RUN   TestVarintRoundTrip
--- PASS: TestVarintRoundTrip (0.00s)
=== RUN   TestVarintKnownEncodings
--- PASS: TestVarintKnownEncodings (0.00s)
=== RUN   TestZigzag
--- PASS: TestZigzag (0.00s)
=== RUN   TestMessageRoundTrip
--- PASS: TestMessageRoundTrip (0.00s)
=== RUN   TestPackedRepeated
--- PASS: TestPackedRepeated (0.00s)
PASS

# Rust
running 4 tests
test tests::varint_round_trip ... ok
test tests::varint_known_encoding ... ok
test tests::zigzag_round_trip ... ok
test tests::message_round_trip ... ok
test result: ok. 4 passed; 0 failed
```

## Design Decisions

1. **Flat byte buffer encoder**: The encoder appends to a `[]byte` / `Vec<u8>` rather than writing to an `io.Writer`. This avoids per-field allocation and system calls. For messages under a few KB (typical gRPC payloads), this is faster than buffered I/O. The buffer can be reused via `Reset()`.

2. **Simplified HPACK**: Full HPACK with dynamic tables and Huffman encoding is a significant implementation effort. Since gRPC uses a small set of well-known headers, literal-without-indexing is sufficient and produces valid HPACK that any compliant decoder can read. This reduces HTTP/2 implementation from weeks to hours.

3. **Stream multiplexing via odd IDs**: Client-initiated streams use odd IDs (1, 3, 5...) per the HTTP/2 spec. The server never initiates streams. This simplification avoids the complexity of server-push while remaining spec-compliant for gRPC's needs.

4. **Synchronous I/O in Go, threaded in Rust**: The Go server uses goroutines per connection (idiomatic Go). The Rust server uses OS threads. A production implementation would use tokio/async in Rust, but threads keep the focus on the protocol rather than the async runtime.

5. **Handler receives raw bytes**: Handlers work with serialized protobuf bytes rather than typed messages. This keeps the framework generic -- message-to-struct deserialization is the application's responsibility. A production framework would code-generate typed stubs.

## Common Mistakes

1. **Varint continuation bit**: The MSB of each byte is a continuation flag, not part of the value. Forgetting to mask with `& 0x7F` corrupts values above 127.

2. **Big-endian vs. little-endian**: gRPC frame lengths are big-endian (network byte order), but protobuf fixed32/fixed64 are little-endian. Mixing them up produces garbage values that look almost correct.

3. **Zigzag off-by-one**: The formula is `(v << 1) ^ (v >> 63)` for 64-bit, not `(v << 1) ^ (v >> 31)`. Using 31 truncates negative values.

4. **HTTP/2 settings acknowledgment**: The server must reply with a SETTINGS frame with the ACK flag after receiving the client's SETTINGS. Omitting this causes the client to hang waiting for the ACK.

5. **gRPC trailers vs. headers**: gRPC status is sent in trailers (HEADERS frame with END_STREAM flag), not in the initial response headers. Sending `grpc-status` in the initial headers is a protocol violation.

## Performance Notes

- **Varint encoding**: 1-10 bytes per integer. Small values (< 128) use 1 byte. This is the primary space advantage over fixed-width encoding for typical message payloads where most integers are small.
- **Frame overhead**: 9 bytes per HTTP/2 frame + 5 bytes per gRPC message frame = 14 bytes overhead per RPC message. For a 100-byte protobuf payload, this is 14% overhead. For 1KB+, negligible.
- **Connection setup**: One TCP handshake + HTTP/2 preface + SETTINGS exchange = 2 round trips before the first RPC. Connection reuse amortizes this across thousands of RPCs.
- **Throughput bottleneck**: In this simplified implementation, the lack of flow control means a fast sender can overwhelm a slow receiver. Production gRPC enforces WINDOW_UPDATE-based flow control.
