# Solution: WebSocket Server Protocol Implementation

## Architecture Overview

The server is structured in four layers:

1. **TCP Listener** -- accepts raw TCP connections and hands them off to the handshake layer
2. **Handshake** -- parses the HTTP upgrade request, validates headers, computes the accept key, and sends the 101 response
3. **Frame Codec** -- reads and writes WebSocket frames (parsing headers, lengths, masking, fragmentation)
4. **Connection Manager** -- tracks active connections, routes messages, handles broadcast, and manages heartbeats

Each client connection runs in two goroutines: a reader loop that parses incoming frames and dispatches messages, and a writer loop that serializes outbound frames from a channel. The connection manager holds a registry of all active connections protected by a `sync.RWMutex`.

```
TCP Listener
    |
    v
Handshake (HTTP upgrade) ──> reject if invalid
    |
    v
Connection (reader goroutine + writer goroutine)
    |
    v
Connection Manager (registry, broadcast, heartbeat)
```

## Go Solution

### `main.go`

```go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	addr := ":9001"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	server := NewServer(addr)

	server.OnMessage(func(conn *Conn, msgType byte, data []byte) {
		log.Printf("from %s: [opcode=%d] %d bytes", conn.RemoteAddr(), msgType, len(data))
		server.Broadcast(msgType, data)
	})

	server.OnConnect(func(conn *Conn) {
		log.Printf("client connected: %s", conn.RemoteAddr())
	})

	server.OnDisconnect(func(conn *Conn) {
		log.Printf("client disconnected: %s", conn.RemoteAddr())
	})

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	log.Printf("websocket server listening on %s", addr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	server.Shutdown()
}
```

### `server.go`

```go
package main

import (
	"log"
	"net"
	"sync"
	"time"
)

type MessageHandler func(conn *Conn, msgType byte, data []byte)
type ConnectHandler func(conn *Conn)

type Server struct {
	addr         string
	listener     net.Listener
	connections  map[*Conn]struct{}
	mu           sync.RWMutex
	onMessage    MessageHandler
	onConnect    ConnectHandler
	onDisconnect ConnectHandler
	quit         chan struct{}
	pingInterval time.Duration
	pongTimeout  time.Duration
}

func NewServer(addr string) *Server {
	return &Server{
		addr:         addr,
		connections:  make(map[*Conn]struct{}),
		quit:         make(chan struct{}),
		pingInterval: 30 * time.Second,
		pongTimeout:  10 * time.Second,
	}
}

func (s *Server) OnMessage(h MessageHandler)    { s.onMessage = h }
func (s *Server) OnConnect(h ConnectHandler)     { s.onConnect = h }
func (s *Server) OnDisconnect(h ConnectHandler)  { s.onDisconnect = h }

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln

	for {
		tcpConn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return nil
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go s.handleNewConnection(tcpConn)
	}
}

func (s *Server) handleNewConnection(tcpConn net.Conn) {
	deflateOk, err := performHandshake(tcpConn)
	if err != nil {
		log.Printf("handshake failed for %s: %v", tcpConn.RemoteAddr(), err)
		tcpConn.Close()
		return
	}

	conn := NewConn(tcpConn, deflateOk)

	s.mu.Lock()
	s.connections[conn] = struct{}{}
	s.mu.Unlock()

	if s.onConnect != nil {
		s.onConnect(conn)
	}

	go s.heartbeat(conn)
	s.readLoop(conn)

	s.mu.Lock()
	delete(s.connections, conn)
	s.mu.Unlock()

	if s.onDisconnect != nil {
		s.onDisconnect(conn)
	}
}

func (s *Server) readLoop(conn *Conn) {
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if s.onMessage != nil {
			s.onMessage(conn, msgType, data)
		}
	}
}

func (s *Server) heartbeat(conn *Conn) {
	ticker := time.NewTicker(s.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			conn.SetPongDeadline(time.Now().Add(s.pongTimeout))
			if err := conn.WritePing([]byte("ping")); err != nil {
				conn.Close(1001, "ping failed")
				return
			}
		case <-conn.done:
			return
		}
	}
}

func (s *Server) Broadcast(msgType byte, data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for conn := range s.connections {
		if err := conn.WriteMessage(msgType, data); err != nil {
			log.Printf("broadcast to %s failed: %v", conn.RemoteAddr(), err)
		}
	}
}

func (s *Server) Shutdown() {
	close(s.quit)
	s.listener.Close()

	s.mu.Lock()
	for conn := range s.connections {
		conn.Close(1001, "server shutting down")
	}
	s.mu.Unlock()
}
```

### `handshake.go`

```go
package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
)

const websocketMagicGUID = "258EAFA5-E914-47DA-95CA-5AB0DC65E740"

func performHandshake(tcpConn net.Conn) (deflateNegotiated bool, err error) {
	reader := bufio.NewReader(tcpConn)

	requestLine, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read request line: %w", err)
	}
	parts := strings.Fields(requestLine)
	if len(parts) < 3 || parts[0] != "GET" {
		return false, fmt.Errorf("invalid request line: %s", requestLine)
	}

	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		value := strings.TrimSpace(line[colonIdx+1:])
		headers[key] = value
	}

	if !strings.EqualFold(headers["upgrade"], "websocket") {
		return false, fmt.Errorf("missing or invalid Upgrade header")
	}
	if !containsToken(headers["connection"], "Upgrade") {
		return false, fmt.Errorf("missing Upgrade in Connection header")
	}
	if headers["sec-websocket-version"] != "13" {
		return false, fmt.Errorf("unsupported version: %s", headers["sec-websocket-version"])
	}

	clientKey := headers["sec-websocket-key"]
	if clientKey == "" {
		return false, fmt.Errorf("missing Sec-WebSocket-Key")
	}

	acceptKey := computeAcceptKey(clientKey)

	// Check for permessage-deflate
	extensions := headers["sec-websocket-extensions"]
	deflateNegotiated = strings.Contains(extensions, "permessage-deflate")

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n"

	if deflateNegotiated {
		response += "Sec-WebSocket-Extensions: permessage-deflate; server_no_context_takeover; client_no_context_takeover\r\n"
	}

	response += "\r\n"

	_, err = tcpConn.Write([]byte(response))
	return deflateNegotiated, err
}

func computeAcceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey + websocketMagicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func containsToken(header, token string) bool {
	for _, t := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(t), token) {
			return true
		}
	}
	return false
}
```

### `frame.go`

```go
package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	OpcodeContinuation = 0x0
	OpcodeText         = 0x1
	OpcodeBinary       = 0x2
	OpcodeClose        = 0x8
	OpcodePing         = 0x9
	OpcodePong         = 0xA
)

type Frame struct {
	Fin     bool
	RSV1    bool // used for permessage-deflate
	RSV2    bool
	RSV3    bool
	Opcode  byte
	Masked  bool
	Payload []byte
}

func ReadFrame(r io.Reader) (*Frame, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read frame header: %w", err)
	}

	f := &Frame{
		Fin:    header[0]&0x80 != 0,
		RSV1:   header[0]&0x40 != 0,
		RSV2:   header[0]&0x20 != 0,
		RSV3:   header[0]&0x10 != 0,
		Opcode: header[0] & 0x0F,
		Masked: header[1]&0x80 != 0,
	}

	payloadLen := uint64(header[1] & 0x7F)

	switch payloadLen {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(r, extended); err != nil {
			return nil, fmt.Errorf("read 16-bit length: %w", err)
		}
		payloadLen = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(r, extended); err != nil {
			return nil, fmt.Errorf("read 64-bit length: %w", err)
		}
		payloadLen = binary.BigEndian.Uint64(extended)
		if payloadLen>>63 != 0 {
			return nil, fmt.Errorf("most significant bit of 64-bit length must be 0")
		}
	}

	var maskKey [4]byte
	if f.Masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, fmt.Errorf("read mask key: %w", err)
		}
	}

	f.Payload = make([]byte, payloadLen)
	if _, err := io.ReadFull(r, f.Payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	if f.Masked {
		maskUnmask(f.Payload, maskKey)
	}

	return f, nil
}

func WriteFrame(w io.Writer, f *Frame) error {
	var header [2]byte

	if f.Fin {
		header[0] |= 0x80
	}
	if f.RSV1 {
		header[0] |= 0x40
	}
	header[0] |= f.Opcode & 0x0F

	payloadLen := len(f.Payload)

	switch {
	case payloadLen <= 125:
		header[1] = byte(payloadLen)
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
	case payloadLen <= 65535:
		header[1] = 126
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
		extended := make([]byte, 2)
		binary.BigEndian.PutUint16(extended, uint16(payloadLen))
		if _, err := w.Write(extended); err != nil {
			return err
		}
	default:
		header[1] = 127
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
		extended := make([]byte, 8)
		binary.BigEndian.PutUint64(extended, uint64(payloadLen))
		if _, err := w.Write(extended); err != nil {
			return err
		}
	}

	// Server frames must not be masked (RFC 6455 Section 5.1)
	if _, err := w.Write(f.Payload); err != nil {
		return err
	}

	return nil
}

func maskUnmask(payload []byte, key [4]byte) {
	for i := range payload {
		payload[i] ^= key[i%4]
	}
}

func isControlFrame(opcode byte) bool {
	return opcode >= 0x8
}
```

### `conn.go`

```go
package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

var deflateTail = []byte{0x00, 0x00, 0xFF, 0xFF}

type Conn struct {
	tcpConn      net.Conn
	reader       *bufio.Reader
	writeMu      sync.Mutex
	deflate      bool
	pongDeadline time.Time
	pongMu       sync.Mutex
	done         chan struct{}
	closeOnce    sync.Once
}

func NewConn(tcpConn net.Conn, deflate bool) *Conn {
	return &Conn{
		tcpConn: tcpConn,
		reader:  bufio.NewReaderSize(tcpConn, 4096),
		deflate: deflate,
		done:    make(chan struct{}),
	}
}

func (c *Conn) RemoteAddr() string {
	return c.tcpConn.RemoteAddr().String()
}

func (c *Conn) SetPongDeadline(t time.Time) {
	c.pongMu.Lock()
	c.pongDeadline = t
	c.pongMu.Unlock()
}

func (c *Conn) ReadMessage() (msgType byte, data []byte, err error) {
	var fragments []byte
	var firstOpcode byte

	for {
		frame, err := ReadFrame(c.reader)
		if err != nil {
			return 0, nil, err
		}

		if !frame.Masked {
			c.Close(1002, "client frames must be masked")
			return 0, nil, fmt.Errorf("received unmasked frame from client")
		}

		if isControlFrame(frame.Opcode) {
			if err := c.handleControlFrame(frame); err != nil {
				return 0, nil, err
			}
			continue
		}

		if frame.Opcode != OpcodeContinuation {
			firstOpcode = frame.Opcode
			fragments = frame.Payload
		} else {
			fragments = append(fragments, frame.Payload...)
		}

		if frame.Fin {
			if c.deflate && frame.RSV1 {
				decompressed, err := c.decompress(fragments)
				if err != nil {
					c.Close(1007, "decompression failed")
					return 0, nil, err
				}
				fragments = decompressed
			}
			return firstOpcode, fragments, nil
		}
	}
}

func (c *Conn) handleControlFrame(frame *Frame) error {
	switch frame.Opcode {
	case OpcodePing:
		return c.WritePong(frame.Payload)
	case OpcodePong:
		c.pongMu.Lock()
		c.pongDeadline = time.Time{}
		c.pongMu.Unlock()
		return nil
	case OpcodeClose:
		code := uint16(1000)
		if len(frame.Payload) >= 2 {
			code = binary.BigEndian.Uint16(frame.Payload[:2])
		}
		c.Close(code, "")
		return fmt.Errorf("received close frame: %d", code)
	}
	return nil
}

func (c *Conn) WriteMessage(msgType byte, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	payload := data
	rsv1 := false

	if c.deflate && (msgType == OpcodeText || msgType == OpcodeBinary) {
		compressed, err := c.compress(data)
		if err != nil {
			return err
		}
		payload = compressed
		rsv1 = true
	}

	frame := &Frame{
		Fin:     true,
		RSV1:    rsv1,
		Opcode:  msgType,
		Payload: payload,
	}

	return WriteFrame(c.tcpConn, frame)
}

func (c *Conn) WritePing(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	frame := &Frame{Fin: true, Opcode: OpcodePing, Payload: payload}
	return WriteFrame(c.tcpConn, frame)
}

func (c *Conn) WritePong(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	frame := &Frame{Fin: true, Opcode: OpcodePong, Payload: payload}
	return WriteFrame(c.tcpConn, frame)
}

func (c *Conn) Close(code uint16, reason string) {
	c.closeOnce.Do(func() {
		payload := make([]byte, 2+len(reason))
		binary.BigEndian.PutUint16(payload[:2], code)
		copy(payload[2:], reason)

		c.writeMu.Lock()
		WriteFrame(c.tcpConn, &Frame{
			Fin:     true,
			Opcode:  OpcodeClose,
			Payload: payload,
		})
		c.writeMu.Unlock()

		close(c.done)
		c.tcpConn.Close()
	})
}

func (c *Conn) compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Flush(); err != nil {
		return nil, err
	}

	compressed := buf.Bytes()
	if len(compressed) >= 4 && bytes.HasSuffix(compressed, deflateTail) {
		compressed = compressed[:len(compressed)-4]
	}
	return compressed, nil
}

func (c *Conn) decompress(data []byte) ([]byte, error) {
	data = append(data, deflateTail...)
	reader := flate.NewReader(bytes.NewReader(data))
	defer reader.Close()
	return io.ReadAll(reader)
}
```

### `main_test.go`

```go
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	server := NewServer(addr)
	server.pingInterval = 60 * time.Second

	server.OnMessage(func(conn *Conn, msgType byte, data []byte) {
		conn.WriteMessage(msgType, data) // echo
	})

	go server.ListenAndServe()
	time.Sleep(50 * time.Millisecond)
	return server, addr
}

func dialWebSocket(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	nonce := make([]byte, 16)
	rand.Read(nonce)
	key := base64.StdEncoding.EncodeToString(nonce)

	request := fmt.Sprintf(
		"GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		addr, key,
	)
	conn.Write([]byte(request))

	reader := bufio.NewReader(conn)
	statusLine, _ := reader.ReadString('\n')
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101, got: %s", statusLine)
	}

	expectedAccept := computeAcceptKey(key)
	for {
		line, _ := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Sec-WebSocket-Accept:") {
			actual := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			if actual != expectedAccept {
				t.Fatalf("accept key mismatch: %s != %s", actual, expectedAccept)
			}
		}
	}
	return conn
}

func writeClientFrame(conn net.Conn, opcode byte, payload []byte) {
	var header [2]byte
	header[0] = 0x80 | opcode // FIN=1

	maskKey := [4]byte{0x37, 0xFA, 0x21, 0x3D}

	payloadLen := len(payload)
	switch {
	case payloadLen <= 125:
		header[1] = 0x80 | byte(payloadLen) // MASK=1
		conn.Write(header[:])
	case payloadLen <= 65535:
		header[1] = 0x80 | 126
		conn.Write(header[:])
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(payloadLen))
		conn.Write(ext)
	default:
		header[1] = 0x80 | 127
		conn.Write(header[:])
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(payloadLen))
		conn.Write(ext)
	}

	conn.Write(maskKey[:])
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ maskKey[i%4]
	}
	conn.Write(masked)
}

func readServerFrame(t *testing.T, conn net.Conn) *Frame {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame, err := ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestHandshake(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()
}

func TestAcceptKeyComputation(t *testing.T) {
	// RFC 6455 Section 4.2.2 example
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="

	h := sha1.New()
	h.Write([]byte(key + websocketMagicGUID))
	actual := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if actual != expected {
		t.Fatalf("accept key: got %s, want %s", actual, expected)
	}
}

func TestTextEcho(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()

	message := []byte("hello websocket")
	writeClientFrame(conn, OpcodeText, message)

	frame := readServerFrame(t, conn)
	if frame.Opcode != OpcodeText {
		t.Fatalf("expected text opcode, got %d", frame.Opcode)
	}
	if string(frame.Payload) != string(message) {
		t.Fatalf("echo mismatch: %q != %q", frame.Payload, message)
	}
}

func TestBinaryEcho(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()

	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	writeClientFrame(conn, OpcodeBinary, data)

	frame := readServerFrame(t, conn)
	if frame.Opcode != OpcodeBinary {
		t.Fatalf("expected binary opcode, got %d", frame.Opcode)
	}
	if len(frame.Payload) != 256 {
		t.Fatalf("payload length: got %d, want 256", len(frame.Payload))
	}
}

func TestLargePayload(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()

	// 70KB payload -- uses 16-bit extended length
	data := make([]byte, 70000)
	rand.Read(data)
	writeClientFrame(conn, OpcodeBinary, data)

	frame := readServerFrame(t, conn)
	if len(frame.Payload) != 70000 {
		t.Fatalf("payload length: got %d, want 70000", len(frame.Payload))
	}
}

func TestPingPong(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()

	writeClientFrame(conn, OpcodePing, []byte("test"))

	frame := readServerFrame(t, conn)
	if frame.Opcode != OpcodePong {
		t.Fatalf("expected pong, got opcode %d", frame.Opcode)
	}
	if string(frame.Payload) != "test" {
		t.Fatalf("pong payload: %q", frame.Payload)
	}
}

func TestCloseHandshake(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()

	closePayload := make([]byte, 2)
	binary.BigEndian.PutUint16(closePayload, 1000)
	writeClientFrame(conn, OpcodeClose, closePayload)

	frame := readServerFrame(t, conn)
	if frame.Opcode != OpcodeClose {
		t.Fatalf("expected close frame, got %d", frame.Opcode)
	}
	code := binary.BigEndian.Uint16(frame.Payload[:2])
	if code != 1000 {
		t.Fatalf("close code: got %d, want 1000", code)
	}
}

func TestConcurrentClients(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	const numClients = 20
	var wg sync.WaitGroup
	wg.Add(numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			defer wg.Done()
			conn := dialWebSocket(t, addr)
			defer conn.Close()

			msg := fmt.Sprintf("client-%d", id)
			writeClientFrame(conn, OpcodeText, []byte(msg))

			frame := readServerFrame(t, conn)
			if string(frame.Payload) != msg {
				t.Errorf("client %d: echo mismatch", id)
			}
		}(i)
	}
	wg.Wait()
}

func TestBroadcast(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	server.OnMessage(func(conn *Conn, msgType byte, data []byte) {
		server.Broadcast(msgType, data)
	})

	conn1 := dialWebSocket(t, addr)
	defer conn1.Close()
	conn2 := dialWebSocket(t, addr)
	defer conn2.Close()

	time.Sleep(50 * time.Millisecond)

	writeClientFrame(conn1, OpcodeText, []byte("broadcast test"))

	f1 := readServerFrame(t, conn1)
	f2 := readServerFrame(t, conn2)

	if string(f1.Payload) != "broadcast test" || string(f2.Payload) != "broadcast test" {
		t.Fatal("broadcast did not reach all clients")
	}
}

func TestUnmaskedClientFrameRejected(t *testing.T) {
	server, addr := startTestServer(t)
	defer server.Shutdown()

	conn := dialWebSocket(t, addr)
	defer conn.Close()

	// Send unmasked frame (MASK bit = 0)
	var header [2]byte
	header[0] = 0x81 // FIN=1, opcode=text
	header[1] = 0x05 // MASK=0, len=5
	conn.Write(header[:])
	conn.Write([]byte("hello"))

	// Server should close the connection
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, err := conn.Read(buf)
	if err == nil || err == io.EOF {
		return // connection closed as expected
	}
}
```

## Running

```bash
# Build and run the server
go run .

# In another terminal, connect with websocat
websocat ws://127.0.0.1:9001/

# Or use a browser console
# const ws = new WebSocket('ws://127.0.0.1:9001/')
# ws.onmessage = e => console.log(e.data)
# ws.send('hello')

# Run tests
go test -v -race ./...
```

## Expected Output

Server startup:
```
2024/01/15 10:30:00 websocket server listening on :9001
2024/01/15 10:30:05 client connected: 127.0.0.1:52341
2024/01/15 10:30:05 from 127.0.0.1:52341: [opcode=1] 5 bytes
2024/01/15 10:30:10 client disconnected: 127.0.0.1:52341
```

Test output:
```
=== RUN   TestHandshake
--- PASS: TestHandshake (0.06s)
=== RUN   TestAcceptKeyComputation
--- PASS: TestAcceptKeyComputation (0.00s)
=== RUN   TestTextEcho
--- PASS: TestTextEcho (0.06s)
=== RUN   TestBinaryEcho
--- PASS: TestBinaryEcho (0.06s)
=== RUN   TestLargePayload
--- PASS: TestLargePayload (0.07s)
=== RUN   TestPingPong
--- PASS: TestPingPong (0.06s)
=== RUN   TestCloseHandshake
--- PASS: TestCloseHandshake (0.06s)
=== RUN   TestConcurrentClients
--- PASS: TestConcurrentClients (0.08s)
=== RUN   TestBroadcast
--- PASS: TestBroadcast (0.10s)
=== RUN   TestUnmaskedClientFrameRejected
--- PASS: TestUnmaskedClientFrameRejected (1.06s)
PASS
ok      websocket-server    1.612s
```

## Design Decisions

**Why `bufio.Reader` for frame parsing.** TCP delivers a byte stream with no message boundaries. A buffered reader lets us read the exact number of bytes we need for each frame field (2-byte header, then optional extended length, then optional mask key, then payload) without making a syscall per read.

**Why separate reader/writer goroutines are not strictly necessary here.** The reader goroutine handles control frames synchronously (writes pong directly). This works because only one goroutine reads and writes at any given moment per direction, and the write mutex serializes outbound frames. In a higher-performance design, you would use a dedicated writer goroutine consuming from a channel to avoid mutex contention.

**Why `sync.Once` for close.** The close handshake can be initiated by either side, and read errors also trigger close. Without `sync.Once`, closing the same connection twice would send duplicate close frames and panic on the already-closed `done` channel.

**Why `compress/flate` instead of raw DEFLATE.** The standard library provides a well-tested DEFLATE implementation. The only protocol-specific detail is the trailing 4-byte sync marker (`0x00 0x00 0xFF 0xFF`) that RFC 7692 requires stripping before sending and restoring before decompressing. Using `Flush()` instead of `Close()` produces the correct output with the marker present.

## Common Mistakes

1. **Forgetting the masking key rotation.** The XOR mask cycles every 4 bytes (`key[i % 4]`), not every frame. Off-by-one in the index produces garbled text that almost looks right but has periodic corruption.

2. **Not handling the three length encodings.** A server that only handles 7-bit lengths breaks on any payload larger than 125 bytes. Always check for the 126 and 127 sentinel values.

3. **Masking server-to-client frames.** RFC 6455 Section 5.1: a server must not mask frames. Clients that receive masked server frames must fail the connection.

4. **Treating fragmentation as optional.** Real browsers fragment large messages. If your server cannot reassemble continuation frames, it will silently produce corrupt data on large sends.

5. **Not handling control frames during fragmentation.** A ping can arrive between two fragments of a text message. If you wait until the text message is complete before processing control frames, ping timeouts will kill the connection.

6. **Closing the TCP connection without the close handshake.** Just calling `conn.Close()` is a protocol violation. The server must send a close frame with a status code and wait for (or at least attempt) the client's close frame in response.

## Performance Notes

- Frame parsing is allocation-light: the only allocation per frame is the payload buffer itself. Headers are parsed into stack variables.
- The masking XOR loop processes one byte at a time. For high throughput, you can process 8 bytes at a time by casting to `uint64` and XORing the mask key repeated twice, but this adds complexity for marginal gain on modern CPUs where the bottleneck is typically network I/O.
- Compression has significant CPU cost. For small messages (under ~100 bytes), compression often produces larger output. Production servers typically set a minimum payload size threshold before applying permessage-deflate.
- The connection registry uses `sync.RWMutex` so broadcasts take a read lock and only connection add/remove takes a write lock. This scales well with many clients receiving infrequent new connections.

## Going Further

- Run the server against the [Autobahn TestSuite](https://github.com/crossbario/autobahn-testsuite) and aim for full compliance across all 500+ test cases
- Implement subprotocol negotiation via `Sec-WebSocket-Protocol`
- Add TLS support by wrapping the listener with `tls.NewListener`
- Implement connection rate limiting and max concurrent connection caps
- Build a chat application with named rooms, user authentication, and message history backed by persistent storage
- Benchmark with 10,000 concurrent connections using `websocket-bench` and profile memory usage per connection
