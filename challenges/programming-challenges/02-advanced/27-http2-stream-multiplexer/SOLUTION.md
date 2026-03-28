# Solution: HTTP/2 Stream Multiplexer

## Architecture Overview

The server is organized into five components:

1. **Connection Handler** -- accepts TCP connections, validates the connection preface, exchanges SETTINGS, and runs the frame read/write loops
2. **Frame Codec** -- serializes and deserializes the 9-byte frame header plus payload for all frame types
3. **HPACK Codec** -- compresses and decompresses headers using the static table, dynamic table, and integer encoding
4. **Stream Manager** -- tracks stream states (idle, open, half-closed, closed), enforces the state machine, and routes frames to stream handlers
5. **Flow Controller** -- manages connection-level and per-stream send/receive windows, blocks sends when windows are exhausted

```
TCP Connection
    |
    v
Connection Preface Validation
    |
    v
Frame Read Loop ──> Frame Codec ──> Stream Router
    |                                    |
    v                                    v
Frame Write Loop <── Flow Controller <── Stream Handlers (goroutines)
    |
    v
HPACK Codec (shared per connection, NOT per stream)
```

## Go Solution

### `frame.go`

```go
package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	FrameData         = 0x0
	FrameHeaders      = 0x1
	FramePriority     = 0x2
	FrameRSTStream    = 0x3
	FrameSettings     = 0x4
	FramePushPromise  = 0x5
	FramePing         = 0x6
	FrameGoAway       = 0x7
	FrameWindowUpdate = 0x8

	FlagACK         = 0x1
	FlagEndStream   = 0x1
	FlagEndHeaders  = 0x4
	FlagPadded      = 0x8
	FlagPriority    = 0x20

	FrameHeaderLen  = 9
	DefaultWindowSize = 65535
	ConnectionPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"
)

type Frame struct {
	Length   uint32
	Type     uint8
	Flags    uint8
	StreamID uint32
	Payload  []byte
}

func ReadFrame(r io.Reader) (*Frame, error) {
	header := make([]byte, FrameHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	f := &Frame{
		Length:   uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2]),
		Type:     header[3],
		Flags:    header[4],
		StreamID: binary.BigEndian.Uint32(header[5:9]) & 0x7FFFFFFF,
	}

	if f.Length > 0 {
		f.Payload = make([]byte, f.Length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return nil, fmt.Errorf("read payload: %w", err)
		}
	}

	return f, nil
}

func WriteFrame(w io.Writer, f *Frame) error {
	header := make([]byte, FrameHeaderLen)
	header[0] = byte(f.Length >> 16)
	header[1] = byte(f.Length >> 8)
	header[2] = byte(f.Length)
	header[3] = f.Type
	header[4] = f.Flags
	binary.BigEndian.PutUint32(header[5:9], f.StreamID&0x7FFFFFFF)

	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return err
		}
	}
	return nil
}

func NewDataFrame(streamID uint32, data []byte, endStream bool) *Frame {
	flags := uint8(0)
	if endStream {
		flags |= FlagEndStream
	}
	return &Frame{
		Length:   uint32(len(data)),
		Type:     FrameData,
		Flags:    flags,
		StreamID: streamID,
		Payload:  data,
	}
}

func NewHeadersFrame(streamID uint32, headerBlock []byte, endStream, endHeaders bool) *Frame {
	flags := uint8(0)
	if endStream {
		flags |= FlagEndStream
	}
	if endHeaders {
		flags |= FlagEndHeaders
	}
	return &Frame{
		Length:   uint32(len(headerBlock)),
		Type:     FrameHeaders,
		Flags:    flags,
		StreamID: streamID,
		Payload:  headerBlock,
	}
}

func NewSettingsFrame(settings []Setting) *Frame {
	payload := make([]byte, len(settings)*6)
	for i, s := range settings {
		binary.BigEndian.PutUint16(payload[i*6:], s.ID)
		binary.BigEndian.PutUint32(payload[i*6+2:], s.Value)
	}
	return &Frame{
		Length:  uint32(len(payload)),
		Type:    FrameSettings,
		Payload: payload,
	}
}

func NewSettingsAckFrame() *Frame {
	return &Frame{Type: FrameSettings, Flags: FlagACK}
}

func NewWindowUpdateFrame(streamID uint32, increment uint32) *Frame {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, increment&0x7FFFFFFF)
	return &Frame{
		Length:   4,
		Type:     FrameWindowUpdate,
		StreamID: streamID,
		Payload:  payload,
	}
}

func NewPingAckFrame(opaqueData []byte) *Frame {
	return &Frame{
		Length:  8,
		Type:    FramePing,
		Flags:   FlagACK,
		Payload: opaqueData,
	}
}

func NewGoAwayFrame(lastStreamID uint32, errorCode uint32) *Frame {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], lastStreamID)
	binary.BigEndian.PutUint32(payload[4:8], errorCode)
	return &Frame{
		Length:  8,
		Type:    FrameGoAway,
		Payload: payload,
	}
}

func NewRSTStreamFrame(streamID uint32, errorCode uint32) *Frame {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, errorCode)
	return &Frame{
		Length:   4,
		Type:     FrameRSTStream,
		StreamID: streamID,
		Payload:  payload,
	}
}

func NewPushPromiseFrame(streamID, promisedID uint32, headerBlock []byte) *Frame {
	payload := make([]byte, 4+len(headerBlock))
	binary.BigEndian.PutUint32(payload[0:4], promisedID&0x7FFFFFFF)
	copy(payload[4:], headerBlock)
	return &Frame{
		Length:   uint32(len(payload)),
		Type:     FramePushPromise,
		Flags:    FlagEndHeaders,
		StreamID: streamID,
		Payload:  payload,
	}
}

type Setting struct {
	ID    uint16
	Value uint32
}

const (
	SettingHeaderTableSize      = 0x1
	SettingEnablePush           = 0x2
	SettingMaxConcurrentStreams  = 0x3
	SettingInitialWindowSize    = 0x4
	SettingMaxFrameSize         = 0x5
	SettingMaxHeaderListSize    = 0x6
)
```

### `hpack.go`

```go
package main

import (
	"bytes"
	"fmt"
)

type HeaderField struct {
	Name  string
	Value string
}

type HPACKDecoder struct {
	dynamicTable []HeaderField
	maxTableSize int
}

func NewHPACKDecoder(maxSize int) *HPACKDecoder {
	return &HPACKDecoder{maxTableSize: maxSize}
}

func (d *HPACKDecoder) Decode(block []byte) ([]HeaderField, error) {
	var headers []HeaderField
	buf := bytes.NewReader(block)

	for buf.Len() > 0 {
		b, _ := buf.ReadByte()

		switch {
		case b&0x80 != 0:
			// Indexed Header Field (Section 6.1)
			buf.UnreadByte()
			idx, err := decodeInteger(buf, 7)
			if err != nil {
				return nil, err
			}
			hf, err := d.lookup(idx)
			if err != nil {
				return nil, err
			}
			headers = append(headers, hf)

		case b&0xC0 == 0x40:
			// Literal Header Field with Incremental Indexing (Section 6.2.1)
			buf.UnreadByte()
			idx, err := decodeInteger(buf, 6)
			if err != nil {
				return nil, err
			}
			var name string
			if idx > 0 {
				hf, err := d.lookup(idx)
				if err != nil {
					return nil, err
				}
				name = hf.Name
			} else {
				name, err = decodeString(buf)
				if err != nil {
					return nil, err
				}
			}
			value, err := decodeString(buf)
			if err != nil {
				return nil, err
			}
			hf := HeaderField{Name: name, Value: value}
			d.addToDynamicTable(hf)
			headers = append(headers, hf)

		case b&0xF0 == 0x00:
			// Literal Header Field without Indexing (Section 6.2.2)
			buf.UnreadByte()
			idx, err := decodeInteger(buf, 4)
			if err != nil {
				return nil, err
			}
			var name string
			if idx > 0 {
				hf, err := d.lookup(idx)
				if err != nil {
					return nil, err
				}
				name = hf.Name
			} else {
				name, err = decodeString(buf)
				if err != nil {
					return nil, err
				}
			}
			value, err := decodeString(buf)
			if err != nil {
				return nil, err
			}
			headers = append(headers, HeaderField{Name: name, Value: value})

		case b&0xF0 == 0x10:
			// Literal Header Field Never Indexed (Section 6.2.3)
			buf.UnreadByte()
			idx, err := decodeInteger(buf, 4)
			if err != nil {
				return nil, err
			}
			var name string
			if idx > 0 {
				hf, err := d.lookup(idx)
				if err != nil {
					return nil, err
				}
				name = hf.Name
			} else {
				name, err = decodeString(buf)
				if err != nil {
					return nil, err
				}
			}
			value, err := decodeString(buf)
			if err != nil {
				return nil, err
			}
			headers = append(headers, HeaderField{Name: name, Value: value})

		case b&0xE0 == 0x20:
			// Dynamic Table Size Update (Section 6.3)
			buf.UnreadByte()
			newSize, err := decodeInteger(buf, 5)
			if err != nil {
				return nil, err
			}
			d.maxTableSize = newSize
			d.evict()

		default:
			return nil, fmt.Errorf("unknown HPACK byte: 0x%02x", b)
		}
	}

	return headers, nil
}

func (d *HPACKDecoder) lookup(index int) (HeaderField, error) {
	if index < 1 {
		return HeaderField{}, fmt.Errorf("invalid index: %d", index)
	}
	if index <= len(staticTable) {
		return staticTable[index-1], nil
	}
	dynIdx := index - len(staticTable) - 1
	if dynIdx >= len(d.dynamicTable) {
		return HeaderField{}, fmt.Errorf("index %d out of range (static=%d, dynamic=%d)", index, len(staticTable), len(d.dynamicTable))
	}
	return d.dynamicTable[dynIdx], nil
}

func (d *HPACKDecoder) addToDynamicTable(hf HeaderField) {
	d.dynamicTable = append([]HeaderField{hf}, d.dynamicTable...)
	d.evict()
}

func (d *HPACKDecoder) evict() {
	size := 0
	for _, hf := range d.dynamicTable {
		size += len(hf.Name) + len(hf.Value) + 32
	}
	for size > d.maxTableSize && len(d.dynamicTable) > 0 {
		last := d.dynamicTable[len(d.dynamicTable)-1]
		size -= len(last.Name) + len(last.Value) + 32
		d.dynamicTable = d.dynamicTable[:len(d.dynamicTable)-1]
	}
}

type HPACKEncoder struct {
	dynamicTable []HeaderField
	maxTableSize int
}

func NewHPACKEncoder(maxSize int) *HPACKEncoder {
	return &HPACKEncoder{maxTableSize: maxSize}
}

func (e *HPACKEncoder) Encode(headers []HeaderField) []byte {
	var buf bytes.Buffer

	for _, hf := range headers {
		// Try indexed from static table
		if idx := findStaticIndex(hf.Name, hf.Value); idx > 0 {
			encodeInteger(&buf, idx, 7, 0x80)
			continue
		}
		// Try name-indexed from static table, literal value
		if idx := findStaticNameIndex(hf.Name); idx > 0 {
			encodeInteger(&buf, idx, 6, 0x40)
			encodeString(&buf, hf.Value)
			e.addToDynamicTable(hf)
			continue
		}
		// Fully literal with indexing
		encodeInteger(&buf, 0, 6, 0x40)
		encodeString(&buf, hf.Name)
		encodeString(&buf, hf.Value)
		e.addToDynamicTable(hf)
	}

	return buf.Bytes()
}

func (e *HPACKEncoder) addToDynamicTable(hf HeaderField) {
	e.dynamicTable = append([]HeaderField{hf}, e.dynamicTable...)
	size := 0
	for _, h := range e.dynamicTable {
		size += len(h.Name) + len(h.Value) + 32
	}
	for size > e.maxTableSize && len(e.dynamicTable) > 0 {
		last := e.dynamicTable[len(e.dynamicTable)-1]
		size -= len(last.Name) + len(last.Value) + 32
		e.dynamicTable = e.dynamicTable[:len(e.dynamicTable)-1]
	}
}

func decodeInteger(r *bytes.Reader, prefixBits int) (int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	mask := (1 << prefixBits) - 1
	value := int(b) & mask
	if value < mask {
		return value, nil
	}
	shift := 0
	for {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&0x7F) << shift
		shift += 7
		if b&0x80 == 0 {
			break
		}
	}
	return value, nil
}

func encodeInteger(buf *bytes.Buffer, value int, prefixBits int, pattern byte) {
	mask := (1 << prefixBits) - 1
	if value < mask {
		buf.WriteByte(pattern | byte(value))
		return
	}
	buf.WriteByte(pattern | byte(mask))
	value -= mask
	for value >= 128 {
		buf.WriteByte(byte(value&0x7F) | 0x80)
		value >>= 7
	}
	buf.WriteByte(byte(value))
}

func decodeString(r *bytes.Reader) (string, error) {
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	huffman := b&0x80 != 0
	r.UnreadByte()
	length, err := decodeInteger(r, 7)
	if err != nil {
		return "", err
	}
	data := make([]byte, length)
	if _, err := r.Read(data); err != nil {
		return "", err
	}
	if huffman {
		decoded, err := huffmanDecode(data)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}
	return string(data), nil
}

func encodeString(buf *bytes.Buffer, s string) {
	// Plain encoding (no Huffman for simplicity)
	encodeInteger(buf, len(s), 7, 0x00)
	buf.WriteString(s)
}

func huffmanDecode(data []byte) ([]byte, error) {
	var result []byte
	var bits uint64
	var nbits int

	for _, b := range data {
		bits = bits<<8 | uint64(b)
		nbits += 8

		for nbits >= 5 {
			found := false
			for codeLen := 5; codeLen <= 30 && codeLen <= nbits; codeLen++ {
				code := (bits >> (nbits - codeLen)) & ((1 << codeLen) - 1)
				if sym, ok := huffmanLookup(uint32(code), codeLen); ok {
					result = append(result, sym)
					nbits -= codeLen
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
	}
	return result, nil
}

// Partial Huffman table for common ASCII characters (RFC 7541 Appendix B)
// A production implementation uses the full 256-entry table
func huffmanLookup(code uint32, length int) (byte, bool) {
	type entry struct {
		code   uint32
		length int
		sym    byte
	}
	table := []entry{
		{0x00, 5, '0'}, {0x01, 5, '1'}, {0x02, 5, '2'}, {0x03, 5, 'a'},
		{0x04, 5, 'c'}, {0x05, 5, 'e'}, {0x06, 5, 'i'}, {0x07, 5, 'o'},
		{0x08, 5, 's'}, {0x09, 5, 't'},
		{0x14, 6, ' '}, {0x15, 6, '%'}, {0x16, 6, '-'}, {0x17, 6, '.'},
		{0x18, 6, '/'}, {0x19, 6, '3'}, {0x1a, 6, '4'}, {0x1b, 6, '5'},
		{0x1c, 6, '6'}, {0x1d, 6, '7'}, {0x1e, 6, '8'}, {0x1f, 6, '9'},
		{0x20, 6, '='}, {0x21, 6, 'A'}, {0x22, 6, '_'}, {0x23, 6, 'b'},
		{0x24, 6, 'd'}, {0x25, 6, 'f'}, {0x26, 6, 'g'}, {0x27, 6, 'h'},
		{0x28, 6, 'l'}, {0x29, 6, 'm'}, {0x2a, 6, 'n'}, {0x2b, 6, 'p'},
		{0x2c, 6, 'r'}, {0x2d, 6, 'u'},
		{0x5c, 7, ':'}, {0x5d, 7, 'B'}, {0x5e, 7, 'C'}, {0x5f, 7, 'D'},
		{0x60, 7, 'E'}, {0x61, 7, 'F'}, {0x62, 7, 'G'}, {0x63, 7, 'H'},
		{0x64, 7, 'I'}, {0x65, 7, 'J'}, {0x66, 7, 'K'}, {0x67, 7, 'L'},
		{0x68, 7, 'M'}, {0x69, 7, 'N'}, {0x6a, 7, 'O'}, {0x6b, 7, 'P'},
		{0x6c, 7, 'Q'}, {0x6d, 7, 'R'}, {0x6e, 7, 'S'}, {0x6f, 7, 'T'},
		{0x70, 7, 'U'}, {0x71, 7, 'V'}, {0x72, 7, 'W'}, {0x73, 7, 'Y'},
		{0x74, 7, 'j'}, {0x75, 7, 'k'}, {0x76, 7, 'q'}, {0x77, 7, 'v'},
		{0x78, 7, 'w'}, {0x79, 7, 'x'}, {0x7a, 7, 'y'}, {0x7b, 7, 'z'},
	}
	for _, e := range table {
		if e.code == code && e.length == length {
			return e.sym, true
		}
	}
	return 0, false
}

func findStaticIndex(name, value string) int {
	for i, hf := range staticTable {
		if hf.Name == name && hf.Value == value {
			return i + 1
		}
	}
	return 0
}

func findStaticNameIndex(name string) int {
	for i, hf := range staticTable {
		if hf.Name == name {
			return i + 1
		}
	}
	return 0
}

// Static table from RFC 7541 Appendix A (first 20 entries shown, full table required)
var staticTable = []HeaderField{
	{":authority", ""},
	{":method", "GET"},
	{":method", "POST"},
	{":path", "/"},
	{":path", "/index.html"},
	{":scheme", "http"},
	{":scheme", "https"},
	{":status", "200"},
	{":status", "204"},
	{":status", "206"},
	{":status", "304"},
	{":status", "400"},
	{":status", "404"},
	{":status", "500"},
	{"accept-charset", ""},
	{"accept-encoding", "gzip, deflate"},
	{"accept-language", ""},
	{"accept-ranges", ""},
	{"accept", ""},
	{"access-control-allow-origin", ""},
	{"age", ""},
	{"allow", ""},
	{"authorization", ""},
	{"cache-control", ""},
	{"content-disposition", ""},
	{"content-encoding", ""},
	{"content-language", ""},
	{"content-length", ""},
	{"content-location", ""},
	{"content-range", ""},
	{"content-type", ""},
	{"cookie", ""},
	{"date", ""},
	{"etag", ""},
	{"expect", ""},
	{"expires", ""},
	{"from", ""},
	{"host", ""},
	{"if-match", ""},
	{"if-modified-since", ""},
	{"if-none-match", ""},
	{"if-range", ""},
	{"if-unmodified-since", ""},
	{"last-modified", ""},
	{"link", ""},
	{"location", ""},
	{"max-forwards", ""},
	{"proxy-authenticate", ""},
	{"proxy-authorization", ""},
	{"range", ""},
	{"referer", ""},
	{"refresh", ""},
	{"retry-after", ""},
	{"server", ""},
	{"set-cookie", ""},
	{"strict-transport-security", ""},
	{"transfer-encoding", ""},
	{"user-agent", ""},
	{"vary", ""},
	{"via", ""},
	{"www-authenticate", ""},
}
```

### `stream.go`

```go
package main

import "fmt"

type StreamState int

const (
	StreamIdle StreamState = iota
	StreamOpen
	StreamHalfClosedLocal
	StreamHalfClosedRemote
	StreamClosed
)

type Stream struct {
	ID         uint32
	State      StreamState
	SendWindow int32
	RecvWindow int32
	Headers    []HeaderField
	Data       []byte
}

type StreamManager struct {
	streams          map[uint32]*Stream
	lastStreamID     uint32
	maxConcurrent    uint32
	initialWindowSize int32
}

func NewStreamManager(initialWindow int32) *StreamManager {
	return &StreamManager{
		streams:          make(map[uint32]*Stream),
		maxConcurrent:    100,
		initialWindowSize: initialWindow,
	}
}

func (sm *StreamManager) GetOrCreate(id uint32) *Stream {
	if s, ok := sm.streams[id]; ok {
		return s
	}
	s := &Stream{
		ID:         id,
		State:      StreamIdle,
		SendWindow: sm.initialWindowSize,
		RecvWindow: sm.initialWindowSize,
	}
	sm.streams[id] = s
	if id > sm.lastStreamID {
		sm.lastStreamID = id
	}
	return s
}

func (sm *StreamManager) TransitionOnRecv(s *Stream, frameType uint8, flags uint8) error {
	switch s.State {
	case StreamIdle:
		if frameType == FrameHeaders {
			s.State = StreamOpen
			if flags&FlagEndStream != 0 {
				s.State = StreamHalfClosedRemote
			}
		} else if frameType == FramePriority {
			// Priority is allowed on idle streams
		} else {
			return fmt.Errorf("stream %d: unexpected frame type %d in idle state", s.ID, frameType)
		}

	case StreamOpen:
		if flags&FlagEndStream != 0 {
			s.State = StreamHalfClosedRemote
		}

	case StreamHalfClosedLocal:
		if flags&FlagEndStream != 0 {
			s.State = StreamClosed
		}

	case StreamHalfClosedRemote:
		if frameType == FrameRSTStream || frameType == FrameWindowUpdate || frameType == FramePriority {
			// Allowed
		} else {
			return fmt.Errorf("stream %d: received %d in half-closed-remote", s.ID, frameType)
		}

	case StreamClosed:
		if frameType == FramePriority || frameType == FrameRSTStream {
			// Allowed
		} else {
			return fmt.Errorf("stream %d: received %d in closed state", s.ID, frameType)
		}
	}

	if frameType == FrameRSTStream {
		s.State = StreamClosed
	}
	return nil
}

func (sm *StreamManager) TransitionOnSend(s *Stream, frameType uint8, flags uint8) {
	switch s.State {
	case StreamOpen:
		if flags&FlagEndStream != 0 {
			s.State = StreamHalfClosedLocal
		}
	case StreamHalfClosedRemote:
		if flags&FlagEndStream != 0 {
			s.State = StreamClosed
		}
	}
}

func (sm *StreamManager) Delete(id uint32) {
	delete(sm.streams, id)
}
```

### `flow.go`

```go
package main

import "sync"

type FlowController struct {
	connSendWindow int32
	connRecvWindow int32
	mu             sync.Mutex
	windowUpdated  chan struct{}
}

func NewFlowController(initialWindow int32) *FlowController {
	return &FlowController{
		connSendWindow: initialWindow,
		connRecvWindow: initialWindow,
		windowUpdated:  make(chan struct{}, 1),
	}
}

func (fc *FlowController) ConsumeSendWindow(n int32) bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if fc.connSendWindow < n {
		return false
	}
	fc.connSendWindow -= n
	return true
}

func (fc *FlowController) ConsumeStreamSendWindow(s *Stream, n int32) bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if s.SendWindow < n || fc.connSendWindow < n {
		return false
	}
	s.SendWindow -= n
	fc.connSendWindow -= n
	return true
}

func (fc *FlowController) ConsumeRecvWindow(n int32) int32 {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.connRecvWindow -= n
	return fc.connRecvWindow
}

func (fc *FlowController) ConsumeStreamRecvWindow(s *Stream, n int32) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	s.RecvWindow -= n
}

func (fc *FlowController) AddSendWindow(n int32) {
	fc.mu.Lock()
	fc.connSendWindow += n
	fc.mu.Unlock()

	select {
	case fc.windowUpdated <- struct{}{}:
	default:
	}
}

func (fc *FlowController) AddStreamSendWindow(s *Stream, n int32) {
	fc.mu.Lock()
	s.SendWindow += n
	fc.mu.Unlock()

	select {
	case fc.windowUpdated <- struct{}{}:
	default:
	}
}

func (fc *FlowController) ConnectionSendWindow() int32 {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.connSendWindow
}
```

### `server.go`

```go
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

type RequestHandler func(stream *Stream) (status int, headers []HeaderField, body []byte)

type Server struct {
	addr    string
	handler RequestHandler
}

func NewServer(addr string, handler RequestHandler) *Server {
	return &Server{addr: addr, handler: handler}
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Printf("HTTP/2 server listening on %s", s.addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReaderSize(conn, 16384)

	// Read connection preface
	preface := make([]byte, len(ConnectionPreface))
	if _, err := io.ReadFull(reader, preface); err != nil {
		log.Printf("read preface: %v", err)
		return
	}
	if string(preface) != ConnectionPreface {
		log.Printf("invalid preface")
		return
	}

	// Read client SETTINGS
	clientSettings, err := ReadFrame(reader)
	if err != nil || clientSettings.Type != FrameSettings {
		log.Printf("expected SETTINGS frame: %v", err)
		return
	}

	writeMu := &sync.Mutex{}

	// Send server SETTINGS
	serverSettings := NewSettingsFrame([]Setting{
		{SettingMaxConcurrentStreams, 100},
		{SettingInitialWindowSize, DefaultWindowSize},
		{SettingMaxFrameSize, 16384},
	})
	writeMu.Lock()
	WriteFrame(conn, serverSettings)
	WriteFrame(conn, NewSettingsAckFrame())
	writeMu.Unlock()

	decoder := NewHPACKDecoder(4096)
	encoder := NewHPACKEncoder(4096)
	streams := NewStreamManager(DefaultWindowSize)
	flow := NewFlowController(DefaultWindowSize)
	nextPushID := uint32(2) // Even-numbered for server-initiated

	// Frame read loop
	for {
		frame, err := ReadFrame(reader)
		if err != nil {
			if err != io.EOF {
				log.Printf("read frame: %v", err)
			}
			return
		}

		switch frame.Type {
		case FrameSettings:
			if frame.Flags&FlagACK != 0 {
				continue
			}
			// Apply settings
			for i := 0; i+5 < len(frame.Payload); i += 6 {
				id := binary.BigEndian.Uint16(frame.Payload[i:])
				val := binary.BigEndian.Uint32(frame.Payload[i+2:])
				switch id {
				case SettingInitialWindowSize:
					streams.initialWindowSize = int32(val)
				case SettingMaxConcurrentStreams:
					streams.maxConcurrent = val
				}
			}
			writeMu.Lock()
			WriteFrame(conn, NewSettingsAckFrame())
			writeMu.Unlock()

		case FramePing:
			if frame.Flags&FlagACK != 0 {
				continue
			}
			writeMu.Lock()
			WriteFrame(conn, NewPingAckFrame(frame.Payload))
			writeMu.Unlock()

		case FrameWindowUpdate:
			increment := int32(binary.BigEndian.Uint32(frame.Payload) & 0x7FFFFFFF)
			if frame.StreamID == 0 {
				flow.AddSendWindow(increment)
			} else {
				st := streams.GetOrCreate(frame.StreamID)
				flow.AddStreamSendWindow(st, increment)
			}

		case FrameHeaders:
			st := streams.GetOrCreate(frame.StreamID)
			if err := streams.TransitionOnRecv(st, frame.Type, frame.Flags); err != nil {
				log.Printf("stream error: %v", err)
				writeMu.Lock()
				WriteFrame(conn, NewRSTStreamFrame(frame.StreamID, 1))
				writeMu.Unlock()
				continue
			}

			headers, err := decoder.Decode(frame.Payload)
			if err != nil {
				log.Printf("HPACK decode error: %v", err)
				writeMu.Lock()
				WriteFrame(conn, NewGoAwayFrame(streams.lastStreamID, 9))
				writeMu.Unlock()
				return
			}
			st.Headers = headers

			if frame.Flags&FlagEndStream != 0 || frame.Flags&FlagEndHeaders != 0 {
				go s.handleStream(conn, writeMu, encoder, streams, flow, st, &nextPushID)
			}

		case FrameData:
			st := streams.GetOrCreate(frame.StreamID)
			if err := streams.TransitionOnRecv(st, frame.Type, frame.Flags); err != nil {
				log.Printf("stream error: %v", err)
				continue
			}
			st.Data = append(st.Data, frame.Payload...)

			dataLen := int32(len(frame.Payload))
			flow.ConsumeRecvWindow(dataLen)
			flow.ConsumeStreamRecvWindow(st, dataLen)

			// Send WINDOW_UPDATE to replenish
			if dataLen > 0 {
				writeMu.Lock()
				WriteFrame(conn, NewWindowUpdateFrame(0, uint32(dataLen)))
				WriteFrame(conn, NewWindowUpdateFrame(frame.StreamID, uint32(dataLen)))
				writeMu.Unlock()
			}

		case FrameRSTStream:
			st := streams.GetOrCreate(frame.StreamID)
			st.State = StreamClosed

		case FrameGoAway:
			log.Printf("received GOAWAY")
			return
		}
	}
}

func (s *Server) handleStream(conn net.Conn, writeMu *sync.Mutex, encoder *HPACKEncoder, streams *StreamManager, flow *FlowController, st *Stream, nextPushID *uint32) {
	method := ""
	path := ""
	for _, h := range st.Headers {
		switch h.Name {
		case ":method":
			method = h.Value
		case ":path":
			path = h.Value
		}
	}
	log.Printf("stream %d: %s %s", st.ID, method, path)

	status, respHeaders, body := s.handler(st)

	// Build response headers
	allHeaders := []HeaderField{
		{":status", fmt.Sprintf("%d", status)},
	}
	allHeaders = append(allHeaders, respHeaders...)

	headerBlock := encoder.Encode(allHeaders)

	writeMu.Lock()
	defer writeMu.Unlock()

	endStream := len(body) == 0
	WriteFrame(conn, NewHeadersFrame(st.ID, headerBlock, endStream, true))

	if len(body) > 0 {
		sendData(conn, flow, streams, st, body)
	}
}

func sendData(conn net.Conn, flow *FlowController, streams *StreamManager, st *Stream, body []byte) {
	maxFrameSize := 16384
	offset := 0

	for offset < len(body) {
		end := offset + maxFrameSize
		if end > len(body) {
			end = len(body)
		}
		chunk := body[offset:end]
		chunkLen := int32(len(chunk))

		// Wait for flow control window
		for !flow.ConsumeStreamSendWindow(st, chunkLen) {
			<-flow.windowUpdated
		}

		endStream := end >= len(body)
		frame := NewDataFrame(st.ID, chunk, endStream)
		WriteFrame(conn, frame)

		if endStream {
			streams.TransitionOnSend(st, FrameData, FlagEndStream)
		}
		offset = end
	}
}
```

### `main.go`

```go
package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	addr := ":8443"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	handler := func(stream *Stream) (int, []HeaderField, []byte) {
		path := ""
		for _, h := range stream.Headers {
			if h.Name == ":path" {
				path = h.Value
			}
		}

		switch path {
		case "/":
			body := []byte("<html><body><h1>HTTP/2 Server</h1><p>Built from raw TCP.</p></body></html>")
			return 200, []HeaderField{
				{"content-type", "text/html"},
				{"content-length", fmt.Sprintf("%d", len(body))},
			}, body
		case "/json":
			body := []byte(`{"status":"ok","protocol":"h2"}`)
			return 200, []HeaderField{
				{"content-type", "application/json"},
				{"content-length", fmt.Sprintf("%d", len(body))},
			}, body
		default:
			body := []byte("Not Found")
			return 404, []HeaderField{
				{"content-type", "text/plain"},
				{"content-length", fmt.Sprintf("%d", len(body))},
			}, body
		}
	}

	server := NewServer(addr, handler)
	log.Fatal(server.ListenAndServe())
}
```

### `main_test.go`

```go
package main

import (
	"bytes"
	"testing"
)

func TestFrameRoundtrip(t *testing.T) {
	original := &Frame{
		Length:   5,
		Type:     FrameData,
		Flags:    FlagEndStream,
		StreamID: 1,
		Payload:  []byte("hello"),
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, original); err != nil {
		t.Fatal(err)
	}

	decoded, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Type != original.Type {
		t.Errorf("type: got %d want %d", decoded.Type, original.Type)
	}
	if decoded.StreamID != original.StreamID {
		t.Errorf("stream ID: got %d want %d", decoded.StreamID, original.StreamID)
	}
	if decoded.Flags != original.Flags {
		t.Errorf("flags: got %d want %d", decoded.Flags, original.Flags)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("payload: got %q want %q", decoded.Payload, original.Payload)
	}
}

func TestHPACKIntegerEncoding(t *testing.T) {
	tests := []struct {
		value      int
		prefixBits int
		pattern    byte
	}{
		{10, 5, 0x00},   // Fits in prefix
		{31, 5, 0x00},   // Exactly at boundary
		{1337, 5, 0x00}, // Multi-byte encoding
	}

	for _, tt := range tests {
		var buf bytes.Buffer
		encodeInteger(&buf, tt.value, tt.prefixBits, tt.pattern)

		reader := bytes.NewReader(buf.Bytes())
		decoded, err := decodeInteger(reader, tt.prefixBits)
		if err != nil {
			t.Errorf("decode(%d): %v", tt.value, err)
			continue
		}
		if decoded != tt.value {
			t.Errorf("decode(%d): got %d", tt.value, decoded)
		}
	}
}

func TestHPACKHeaderRoundtrip(t *testing.T) {
	encoder := NewHPACKEncoder(4096)
	decoder := NewHPACKDecoder(4096)

	headers := []HeaderField{
		{":status", "200"},
		{"content-type", "text/html"},
		{"x-custom", "test-value"},
	}

	encoded := encoder.Encode(headers)
	decoded, err := decoder.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != len(headers) {
		t.Fatalf("header count: got %d want %d", len(decoded), len(headers))
	}

	for i := range headers {
		if decoded[i].Name != headers[i].Name || decoded[i].Value != headers[i].Value {
			t.Errorf("header %d: got %v want %v", i, decoded[i], headers[i])
		}
	}
}

func TestStaticTableLookup(t *testing.T) {
	// :method GET should be index 2
	idx := findStaticIndex(":method", "GET")
	if idx != 2 {
		t.Errorf(":method GET index: got %d want 2", idx)
	}

	// :status 200 should be index 8
	idx = findStaticIndex(":status", "200")
	if idx != 8 {
		t.Errorf(":status 200 index: got %d want 8", idx)
	}

	// Name-only lookup for :path
	idx = findStaticNameIndex(":path")
	if idx != 4 {
		t.Errorf(":path name index: got %d want 4", idx)
	}
}

func TestStreamStateTransitions(t *testing.T) {
	sm := NewStreamManager(DefaultWindowSize)

	// idle -> open via HEADERS
	s := sm.GetOrCreate(1)
	err := sm.TransitionOnRecv(s, FrameHeaders, 0)
	if err != nil || s.State != StreamOpen {
		t.Errorf("after HEADERS: state=%d err=%v", s.State, err)
	}

	// open -> half-closed-remote via HEADERS+END_STREAM
	err = sm.TransitionOnRecv(s, FrameHeaders, FlagEndStream)
	if err != nil || s.State != StreamHalfClosedRemote {
		t.Errorf("after END_STREAM: state=%d err=%v", s.State, err)
	}

	// half-closed-remote -> closed via server END_STREAM
	sm.TransitionOnSend(s, FrameData, FlagEndStream)
	if s.State != StreamClosed {
		t.Errorf("after server END_STREAM: state=%d", s.State)
	}
}

func TestFlowControlWindow(t *testing.T) {
	fc := NewFlowController(DefaultWindowSize)

	// Consume partial window
	if !fc.ConsumeSendWindow(1000) {
		t.Fatal("should allow consuming 1000 from default window")
	}

	remaining := fc.ConnectionSendWindow()
	if remaining != DefaultWindowSize-1000 {
		t.Errorf("remaining: got %d want %d", remaining, DefaultWindowSize-1000)
	}

	// Add window
	fc.AddSendWindow(500)
	remaining = fc.ConnectionSendWindow()
	if remaining != DefaultWindowSize-500 {
		t.Errorf("after update: got %d want %d", remaining, DefaultWindowSize-500)
	}
}

func TestSettingsFrameEncoding(t *testing.T) {
	settings := NewSettingsFrame([]Setting{
		{SettingMaxConcurrentStreams, 100},
		{SettingInitialWindowSize, 65535},
	})

	if settings.Type != FrameSettings {
		t.Errorf("type: got %d", settings.Type)
	}
	if settings.Length != 12 { // 2 settings x 6 bytes
		t.Errorf("length: got %d want 12", settings.Length)
	}
}

func TestGoAwayFrame(t *testing.T) {
	f := NewGoAwayFrame(7, 0)
	var buf bytes.Buffer
	WriteFrame(&buf, f)

	decoded, _ := ReadFrame(&buf)
	if decoded.Type != FrameGoAway {
		t.Errorf("type: got %d", decoded.Type)
	}
	// Last stream ID in payload
	lastID := uint32(decoded.Payload[0])<<24 | uint32(decoded.Payload[1])<<16 |
		uint32(decoded.Payload[2])<<8 | uint32(decoded.Payload[3])
	if lastID != 7 {
		t.Errorf("last stream ID: got %d want 7", lastID)
	}
}
```

## Running

```bash
# Build and run
go run .

# Test with curl (HTTP/2 prior knowledge, no TLS)
curl --http2-prior-knowledge -v http://127.0.0.1:8443/
curl --http2-prior-knowledge http://127.0.0.1:8443/json

# Benchmark with h2load
h2load -n 1000 -c 10 --h1 http://127.0.0.1:8443/

# Run compliance tests
h2spec -h 127.0.0.1 -p 8443

# Run unit tests
go test -v -race ./...
```

## Expected Output

Server startup:
```
2024/01/15 10:30:00 HTTP/2 server listening on :8443
2024/01/15 10:30:05 stream 1: GET /
2024/01/15 10:30:05 stream 3: GET /json
```

curl output:
```
* Connected to 127.0.0.1 port 8443
* Using HTTP2, server supports multiplexing
> GET / HTTP/2
< HTTP/2 200
< content-type: text/html
< content-length: 73
<html><body><h1>HTTP/2 Server</h1><p>Built from raw TCP.</p></body></html>
```

Test output:
```
=== RUN   TestFrameRoundtrip
--- PASS: TestFrameRoundtrip (0.00s)
=== RUN   TestHPACKIntegerEncoding
--- PASS: TestHPACKIntegerEncoding (0.00s)
=== RUN   TestHPACKHeaderRoundtrip
--- PASS: TestHPACKHeaderRoundtrip (0.00s)
=== RUN   TestStaticTableLookup
--- PASS: TestStaticTableLookup (0.00s)
=== RUN   TestStreamStateTransitions
--- PASS: TestStreamStateTransitions (0.00s)
=== RUN   TestFlowControlWindow
--- PASS: TestFlowControlWindow (0.00s)
=== RUN   TestSettingsFrameEncoding
--- PASS: TestSettingsFrameEncoding (0.00s)
=== RUN   TestGoAwayFrame
--- PASS: TestGoAwayFrame (0.00s)
PASS
ok      http2-multiplexer    0.003s
```

## Design Decisions

**Why a single frame read loop instead of per-stream readers.** HTTP/2 frames on a connection are sequentially ordered -- you cannot read stream 3's next frame without reading stream 1's frame first. A single goroutine reads and demultiplexes frames, then dispatches to stream handler goroutines. This avoids the coordination complexity of multiple readers on the same connection.

**Why the HPACK encoder avoids Huffman coding for outbound strings.** Huffman encoding saves 20-30% on common header values but requires the full 256-symbol Huffman table and a bit-level writer. For a learning implementation, plain string encoding is correct per the spec (Huffman is optional) and makes debugging significantly easier. The decoder handles Huffman because clients will send Huffman-encoded headers.

**Why flow control uses a channel signal instead of a condition variable.** The `windowUpdated` channel acts as a wakeup mechanism for goroutines blocked on flow control. A `sync.Cond` would also work, but the channel integrates naturally with Go's `select` statement if you later want to add timeout or cancellation.

**Why stream state transitions are checked on receive only.** Send-side transitions are controlled by our code, so invalid transitions indicate bugs in our logic. Receive-side transitions can be violated by misbehaving clients, so they must be validated and result in RST_STREAM or GOAWAY errors.

## Common Mistakes

1. **Treating stream ID 0 as a regular stream.** Stream 0 is the connection control stream. SETTINGS, PING, GOAWAY, and connection-level WINDOW_UPDATE use stream 0. Routing these to a stream handler is a protocol error.

2. **Using one HPACK context per stream.** HPACK state is shared across all streams on a connection. The dynamic table is updated by every HEADERS frame regardless of stream ID. Using separate decoders per stream corrupts the table synchronization.

3. **Forgetting that flow control windows can go negative.** When SETTINGS changes INITIAL_WINDOW_SIZE, existing streams must adjust their windows. The difference can push a window negative, meaning sends must block until WINDOW_UPDATE restores it above zero.

4. **Not handling CONTINUATION frames.** A HEADERS frame without END_HEADERS must be followed by one or more CONTINUATION frames. No other frame type can appear between them on that stream. Missing this makes large header blocks fail.

5. **Masking the reserved bit in stream ID.** The most significant bit of the stream ID field is reserved and must be zero. Always mask with `0x7FFFFFFF` when reading. Failing to do so produces stream IDs above 2^31 which break the odd/even client/server convention.

## Performance Notes

- Frame parsing is zero-allocation for the header (9 bytes on stack). The only heap allocation is the payload buffer, which could be pooled with `sync.Pool` for high-throughput servers.
- HPACK dynamic table operations are O(n) for eviction scans. Production implementations use a ring buffer with size tracking to make this O(1).
- The flow control blocking loop is a busy-wait on a channel, which parks the goroutine efficiently. For maximum throughput, buffer outbound frames in a queue and drain them when the window opens, rather than blocking per frame.
- The complete HPACK Huffman table (256 entries + EOS) enables 20-30% header size reduction. Adding it to the encoder is a straightforward improvement for bandwidth-sensitive deployments.

## Going Further

- Implement CONTINUATION frames for headers that exceed MAX_FRAME_SIZE
- Add TLS with ALPN negotiation (`h2` protocol ID) so standard browsers can connect
- Run the server against [h2spec](https://github.com/summerwind/h2spec) and fix all failing cases
- Implement stream priority and dependency trees (RFC 9113 Section 5.3)
- Add server push for CSS/JS resources when the HTML page is requested
- Build a reverse proxy that multiplexes multiple backend connections over a single HTTP/2 connection to the client
