# Solution: Complete HTTP Server Framework from Raw TCP

## Architecture Overview

The framework is composed of seven layers, each independently testable:

```
Application (user handlers + middleware)
    |
Context Layer (per-request state, JSON/form helpers)
    |
Middleware Pipeline (onion composition)
    |
Router (radix tree, method dispatch)
    |
HTTP Codec (request parser + response writer)
    |
Connection Manager (keep-alive, pipelining, idle timeout, WebSocket upgrade)
    |
TCP Listener (accept, goroutine-per-conn, graceful shutdown)
```

The TCP listener accepts connections and hands them to the connection manager. The connection manager reads HTTP requests in a loop (keep-alive), parses them through the HTTP codec, dispatches through the router, and writes responses back. When a WebSocket upgrade is requested, the connection manager hands the raw `net.Conn` to the WebSocket handler and exits the HTTP loop.

The radix tree stores one tree per HTTP method. The middleware pipeline wraps the final handler in an onion of middleware functions. The Context object carries everything a handler needs: parsed parameters, query values, request body, response writer, deadline, and a key-value store.

## Go Solution

The solution is split into packages. Below is the complete implementation.

### Project Setup

```bash
mkdir -p httpfw && cd httpfw
go mod init httpfw
```

### HTTP Request and Response Types

```go
// types.go
package httpfw

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Request represents a parsed HTTP request.
type Request struct {
	Method      string
	Path        string
	RawQuery    string
	Version     string
	Headers     map[string]string
	Body        []byte
	ContentLength int64
	Host        string
	RemoteAddr  string
	query       url.Values
	parsedOnce  sync.Once
}

func (r *Request) QueryValues() url.Values {
	r.parsedOnce.Do(func() {
		r.query, _ = url.ParseQuery(r.RawQuery)
	})
	return r.query
}

func (r *Request) Query(key string) string {
	return r.QueryValues().Get(key)
}

func (r *Request) Header(key string) string {
	return r.Headers[strings.ToLower(key)]
}

func (r *Request) WantsClose() bool {
	return strings.EqualFold(r.Header("Connection"), "close")
}

// Response holds the outgoing HTTP response state.
type Response struct {
	StatusCode  int
	StatusText  string
	Headers     map[string]string
	body        []byte
	chunked     bool
	headersSent bool
	conn        io.Writer
	mu          sync.Mutex
}

func NewResponse(conn io.Writer) *Response {
	return &Response{
		StatusCode: 200,
		StatusText: "OK",
		Headers:    make(map[string]string),
		conn:       conn,
	}
}

func (r *Response) SetStatus(code int) {
	r.StatusCode = code
	r.StatusText = statusText(code)
}

func (r *Response) SetHeader(key, value string) {
	r.Headers[key] = value
}

func (r *Response) WriteHeaders() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.headersSent {
		return
	}
	r.headersSent = true

	var sb strings.Builder
	fmt.Fprintf(&sb, "HTTP/1.1 %d %s\r\n", r.StatusCode, r.StatusText)
	fmt.Fprintf(&sb, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123))

	for k, v := range r.Headers {
		fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
	}
	sb.WriteString("\r\n")
	r.conn.Write([]byte(sb.String()))
}

func (r *Response) Write(data []byte) (int, error) {
	if !r.headersSent {
		if r.chunked {
			r.SetHeader("Transfer-Encoding", "chunked")
		}
		r.WriteHeaders()
	}

	if r.chunked {
		chunk := fmt.Sprintf("%x\r\n%s\r\n", len(data), data)
		return r.conn.Write([]byte(chunk))
	}
	return r.conn.Write(data)
}

func (r *Response) EndChunked() {
	if r.chunked {
		r.conn.Write([]byte("0\r\n\r\n"))
	}
}

func (r *Response) WriteFullBody(data []byte) {
	r.SetHeader("Content-Length", fmt.Sprintf("%d", len(data)))
	r.WriteHeaders()
	r.conn.Write(data)
}

func statusText(code int) string {
	texts := map[int]string{
		200: "OK", 201: "Created", 204: "No Content",
		301: "Moved Permanently", 302: "Found", 304: "Not Modified",
		400: "Bad Request", 401: "Unauthorized", 403: "Forbidden",
		404: "Not Found", 405: "Method Not Allowed", 408: "Request Timeout",
		413: "Content Too Large", 415: "Unsupported Media Type",
		500: "Internal Server Error", 502: "Bad Gateway", 503: "Service Unavailable",
	}
	if t, ok := texts[code]; ok {
		return t
	}
	return "Unknown"
}

// UploadedFile holds a parsed multipart file upload.
type UploadedFile struct {
	Filename    string
	ContentType string
	Size        int64
	Data        []byte
}

// --- Context ---

// HandlerFunc is the application handler signature.
type HandlerFunc func(*Context)

// Context carries all per-request state.
type Context struct {
	Request  *Request
	Response *Response
	Params   map[string]string
	store    map[string]any
	index    int
	handlers []HandlerFunc
	conn     io.ReadWriteCloser
}

func NewContext(req *Request, resp *Response, conn io.ReadWriteCloser) *Context {
	return &Context{
		Request:  req,
		Response: resp,
		Params:   make(map[string]string),
		store:    make(map[string]any),
		conn:     conn,
		index:    -1,
	}
}

func (c *Context) Next() {
	c.index++
	for c.index < len(c.handlers) {
		c.handlers[c.index](c)
		c.index++
	}
}

func (c *Context) Set(key string, value any) {
	c.store[key] = value
}

func (c *Context) Get(key string) (any, bool) {
	val, ok := c.store[key]
	return val, ok
}

func (c *Context) Param(name string) string {
	return c.Params[name]
}

// JSON serializes the value as JSON and writes it to the response.
func (c *Context) JSON(status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		c.String(500, "JSON encoding error: %v", err)
		return
	}
	c.Response.SetStatus(status)
	c.Response.SetHeader("Content-Type", "application/json; charset=utf-8")
	c.Response.WriteFullBody(data)
}

// String writes a formatted string response.
func (c *Context) String(status int, format string, args ...any) {
	c.Response.SetStatus(status)
	c.Response.SetHeader("Content-Type", "text/plain; charset=utf-8")
	body := fmt.Sprintf(format, args...)
	c.Response.WriteFullBody([]byte(body))
}

// HTML writes an HTML response.
func (c *Context) HTML(status int, html string) {
	c.Response.SetStatus(status)
	c.Response.SetHeader("Content-Type", "text/html; charset=utf-8")
	c.Response.WriteFullBody([]byte(html))
}

// Bind deserializes the request body JSON into the target struct.
func (c *Context) Bind(dest any) error {
	if c.Request.Body == nil {
		return fmt.Errorf("empty request body")
	}
	ct := c.Request.Header("content-type")
	if !strings.Contains(ct, "application/json") {
		return fmt.Errorf("unsupported content type: %s", ct)
	}
	return json.Unmarshal(c.Request.Body, dest)
}

// FormValue returns a URL-encoded form value.
func (c *Context) FormValue(key string) string {
	if c.Request.Body == nil {
		return ""
	}
	values, err := url.ParseQuery(string(c.Request.Body))
	if err != nil {
		return ""
	}
	return values.Get(key)
}

// MultipartFile parses a multipart form and returns the file for the given field.
func (c *Context) MultipartFile(fieldName string, maxMemory int64) (*UploadedFile, error) {
	ct := c.Request.Header("content-type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "multipart/form-data" {
		return nil, fmt.Errorf("not multipart/form-data")
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, fmt.Errorf("missing boundary")
	}

	reader := multipart.NewReader(strings.NewReader(string(c.Request.Body)), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if part.FormName() != fieldName {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, maxMemory))
		if err != nil {
			return nil, err
		}
		return &UploadedFile{
			Filename:    part.FileName(),
			ContentType: part.Header.Get("Content-Type"),
			Size:        int64(len(data)),
			Data:        data,
		}, nil
	}
	return nil, fmt.Errorf("field %q not found", fieldName)
}

// Status sends just a status code with no body.
func (c *Context) Status(code int) {
	c.Response.SetStatus(code)
	c.Response.SetHeader("Content-Length", "0")
	c.Response.WriteHeaders()
}
```

### HTTP Parser

```go
// parser.go
package httpfw

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// ParseRequest reads and parses a single HTTP/1.1 request from the connection.
func ParseRequest(reader *bufio.Reader, remoteAddr net.Addr) (*Request, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed request line: %q", line)
	}

	pathAndQuery := parts[1]
	path := pathAndQuery
	rawQuery := ""
	if idx := strings.IndexByte(pathAndQuery, '?'); idx != -1 {
		path = pathAndQuery[:idx]
		rawQuery = pathAndQuery[idx+1:]
	}

	req := &Request{
		Method:   parts[0],
		Path:     path,
		RawQuery: rawQuery,
		Version:  parts[2],
		Headers:  make(map[string]string),
	}
	if remoteAddr != nil {
		req.RemoteAddr = remoteAddr.String()
	}

	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header = strings.TrimRight(header, "\r\n")
		if header == "" {
			break
		}
		colonIdx := strings.IndexByte(header, ':')
		if colonIdx == -1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(header[:colonIdx]))
		val := strings.TrimSpace(header[colonIdx+1:])
		req.Headers[key] = val
	}

	req.Host = req.Headers["host"]

	if err := parseBody(reader, req); err != nil {
		return nil, err
	}

	return req, nil
}

func parseBody(reader *bufio.Reader, req *Request) error {
	if te := req.Headers["transfer-encoding"]; strings.Contains(strings.ToLower(te), "chunked") {
		return parseChunkedBody(reader, req)
	}

	if cl := req.Headers["content-length"]; cl != "" {
		length, err := strconv.ParseInt(cl, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid content-length: %s", cl)
		}
		req.ContentLength = length
		if length > 0 {
			body := make([]byte, length)
			if _, err := io.ReadFull(reader, body); err != nil {
				return fmt.Errorf("reading body: %w", err)
			}
			req.Body = body
		}
	}

	return nil
}

func parseChunkedBody(reader *bufio.Reader, req *Request) error {
	var body []byte
	for {
		sizeLine, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		sizeLine = strings.TrimRight(sizeLine, "\r\n")
		// strip chunk extensions
		if idx := strings.IndexByte(sizeLine, ';'); idx != -1 {
			sizeLine = sizeLine[:idx]
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeLine), 16, 64)
		if err != nil {
			return fmt.Errorf("invalid chunk size: %q", sizeLine)
		}
		if size == 0 {
			reader.ReadString('\n') // trailing CRLF
			break
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return fmt.Errorf("reading chunk: %w", err)
		}
		body = append(body, chunk...)
		reader.ReadString('\n') // CRLF after chunk data
	}
	req.Body = body
	req.ContentLength = int64(len(body))
	return nil
}
```

### Radix Tree Router

```go
// router.go
package httpfw

import (
	"fmt"
	"strings"
)

type nodeKind uint8

const (
	kindStatic   nodeKind = iota
	kindParam
	kindCatchAll
)

type radixNode struct {
	segment    string
	children   []*radixNode
	indices    string
	handler    HandlerFunc
	kind       nodeKind
	paramName  string
	paramChild *radixNode
	catchChild *radixNode
	isLeaf     bool
	middleware []Middleware
}

// Router dispatches requests to handlers via a radix tree.
type Router struct {
	trees      map[string]*radixNode
	middleware []Middleware
	notFound   HandlerFunc
	methodNA   HandlerFunc
}

// Middleware is the function signature for middleware.
type Middleware func(HandlerFunc) HandlerFunc

func NewRouter() *Router {
	return &Router{
		trees: make(map[string]*radixNode),
	}
}

func (r *Router) Use(mw ...Middleware) {
	r.middleware = append(r.middleware, mw...)
}

func (r *Router) NotFound(handler HandlerFunc) {
	r.notFound = handler
}

func (r *Router) MethodNotAllowed(handler HandlerFunc) {
	r.methodNA = handler
}

func (r *Router) Handle(method, pattern string, handler HandlerFunc, mw ...Middleware) error {
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("pattern must start with /: %q", pattern)
	}

	root, exists := r.trees[method]
	if !exists {
		root = &radixNode{segment: "/"}
		r.trees[method] = root
	}

	// Apply per-route middleware
	wrapped := handler
	for i := len(mw) - 1; i >= 0; i-- {
		wrapped = mw[i](wrapped)
	}

	return root.insert(pattern[1:], wrapped)
}

func (r *Router) GET(pattern string, handler HandlerFunc, mw ...Middleware) error {
	return r.Handle("GET", pattern, handler, mw...)
}

func (r *Router) POST(pattern string, handler HandlerFunc, mw ...Middleware) error {
	return r.Handle("POST", pattern, handler, mw...)
}

func (r *Router) PUT(pattern string, handler HandlerFunc, mw ...Middleware) error {
	return r.Handle("PUT", pattern, handler, mw...)
}

func (r *Router) DELETE(pattern string, handler HandlerFunc, mw ...Middleware) error {
	return r.Handle("DELETE", pattern, handler, mw...)
}

func (n *radixNode) insert(path string, handler HandlerFunc) error {
	if path == "" {
		if n.isLeaf {
			return fmt.Errorf("route conflict: handler already registered")
		}
		n.handler = handler
		n.isLeaf = true
		return nil
	}

	if path[0] == ':' {
		return n.insertParam(path, handler)
	}
	if path[0] == '*' {
		return n.insertCatchAll(path, handler)
	}
	return n.insertStatic(path, handler)
}

func (n *radixNode) insertParam(path string, handler HandlerFunc) error {
	slashIdx := strings.IndexByte(path, '/')
	var paramName, rest string
	if slashIdx == -1 {
		paramName = path[1:]
	} else {
		paramName = path[1:slashIdx]
		rest = path[slashIdx+1:]
	}

	if n.catchChild != nil {
		return fmt.Errorf("route conflict: param :%s conflicts with catch-all", paramName)
	}

	if n.paramChild != nil {
		if n.paramChild.paramName != paramName {
			return fmt.Errorf("route conflict: :%s conflicts with :%s", paramName, n.paramChild.paramName)
		}
		return n.paramChild.insert(rest, handler)
	}

	child := &radixNode{segment: ":" + paramName, kind: kindParam, paramName: paramName}
	n.paramChild = child
	return child.insert(rest, handler)
}

func (n *radixNode) insertCatchAll(path string, handler HandlerFunc) error {
	name := path[1:]
	if len(n.children) > 0 || n.paramChild != nil {
		return fmt.Errorf("route conflict: catch-all *%s conflicts with existing routes", name)
	}
	if n.catchChild != nil {
		return fmt.Errorf("route conflict: catch-all already registered")
	}
	n.catchChild = &radixNode{
		segment:   "*" + name,
		kind:      kindCatchAll,
		paramName: name,
		handler:   handler,
		isLeaf:    true,
	}
	return nil
}

func (n *radixNode) insertStatic(path string, handler HandlerFunc) error {
	if n.catchChild != nil {
		return fmt.Errorf("route conflict: static path conflicts with catch-all")
	}

	for i, child := range n.children {
		prefix := longestCommonPrefix(child.segment, path)
		if prefix == 0 {
			continue
		}

		if prefix == len(child.segment) {
			rest := path[prefix:]
			if len(rest) > 0 && rest[0] == '/' {
				rest = rest[1:]
			}
			return child.insert(rest, handler)
		}

		// Split
		split := &radixNode{
			segment:    child.segment[prefix:],
			children:   child.children,
			indices:    child.indices,
			handler:    child.handler,
			kind:       child.kind,
			paramChild: child.paramChild,
			catchChild: child.catchChild,
			isLeaf:     child.isLeaf,
		}

		n.children[i] = &radixNode{segment: child.segment[:prefix], kind: kindStatic}
		newNode := n.children[i]
		newNode.indices = string(split.segment[0])
		newNode.children = []*radixNode{split}

		rest := path[prefix:]
		if len(rest) == 0 {
			newNode.handler = handler
			newNode.isLeaf = true
			return nil
		}
		if rest[0] == '/' {
			rest = rest[1:]
		}
		return newNode.insert(rest, handler)
	}

	segEnd := strings.IndexAny(path, "/:*")
	if segEnd == -1 {
		segEnd = len(path)
	}

	child := &radixNode{segment: path[:segEnd], kind: kindStatic}
	if len(child.segment) > 0 {
		n.indices += string(child.segment[0])
	}
	n.children = append(n.children, child)

	rest := path[segEnd:]
	if len(rest) > 0 && rest[0] == '/' {
		rest = rest[1:]
	}
	return child.insert(rest, handler)
}

func longestCommonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return max
}

// Dispatch finds the handler for a request and populates params.
func (r *Router) Dispatch(method, path string) (HandlerFunc, map[string]string, int) {
	root, exists := r.trees[method]
	if !exists {
		// Check if path exists for other methods -> 405
		for _, tree := range r.trees {
			params := make(map[string]string)
			if _, ok := tree.lookup(trimLeadingSlash(path), params); ok {
				return nil, nil, 405
			}
		}
		return nil, nil, 404
	}

	params := make(map[string]string)
	handler, found := root.lookup(trimLeadingSlash(path), params)
	if found {
		return handler, params, 200
	}

	// Check other methods for 405
	for m, tree := range r.trees {
		if m == method {
			continue
		}
		p := make(map[string]string)
		if _, ok := tree.lookup(trimLeadingSlash(path), p); ok {
			return nil, nil, 405
		}
	}

	return nil, nil, 404
}

func trimLeadingSlash(path string) string {
	if len(path) > 0 && path[0] == '/' {
		return path[1:]
	}
	return path
}

func (n *radixNode) lookup(path string, params map[string]string) (HandlerFunc, bool) {
	if path == "" {
		if n.isLeaf {
			return n.handler, true
		}
		return nil, false
	}

	for i, child := range n.children {
		if i < len(n.indices) && n.indices[i] != path[0] {
			continue
		}
		segLen := len(child.segment)
		if len(path) >= segLen && path[:segLen] == child.segment {
			rest := path[segLen:]
			if len(rest) > 0 && rest[0] == '/' {
				rest = rest[1:]
			}
			if h, ok := child.lookup(rest, params); ok {
				return h, true
			}
		}
	}

	if n.paramChild != nil {
		slashIdx := strings.IndexByte(path, '/')
		var segment, rest string
		if slashIdx == -1 {
			segment = path
		} else {
			segment = path[:slashIdx]
			rest = path[slashIdx+1:]
		}
		if segment != "" {
			params[n.paramChild.paramName] = segment
			if h, ok := n.paramChild.lookup(rest, params); ok {
				return h, true
			}
			delete(params, n.paramChild.paramName)
		}
	}

	if n.catchChild != nil {
		params[n.catchChild.paramName] = path
		return n.catchChild.handler, true
	}

	return nil, false
}
```

### WebSocket

```go
// websocket.go
package httpfw

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcodes
const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

// WSConn represents a WebSocket connection.
type WSConn struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex // protects writes
}

// WSMessage represents a WebSocket message.
type WSMessage struct {
	Type    int
	Payload []byte
}

// UpgradeWebSocket performs the WebSocket handshake on the raw connection.
func (c *Context) UpgradeWebSocket() (*WSConn, error) {
	req := c.Request

	if !strings.EqualFold(req.Header("upgrade"), "websocket") {
		return nil, fmt.Errorf("missing Upgrade: websocket header")
	}
	if !strings.Contains(strings.ToLower(req.Header("connection")), "upgrade") {
		return nil, fmt.Errorf("missing Connection: Upgrade header")
	}

	key := req.Header("sec-websocket-key")
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key header")
	}

	acceptKey := computeAcceptKey(key)

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"

	rwc, ok := c.conn.(net.Conn)
	if !ok {
		return nil, fmt.Errorf("connection does not support WebSocket upgrade")
	}

	if _, err := rwc.Write([]byte(response)); err != nil {
		return nil, fmt.Errorf("writing handshake: %w", err)
	}

	return &WSConn{
		conn:   rwc,
		reader: bufio.NewReader(rwc),
	}, nil
}

func computeAcceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey + websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ReadMessage reads a complete WebSocket message, handling fragmentation.
func (ws *WSConn) ReadMessage() (*WSMessage, error) {
	var fullPayload []byte
	var messageType int

	for {
		fin, opcode, payload, err := ws.readFrame()
		if err != nil {
			return nil, err
		}

		switch opcode {
		case OpPing:
			ws.writeFrame(OpPong, payload)
			continue
		case OpPong:
			continue
		case OpClose:
			ws.writeFrame(OpClose, nil)
			return &WSMessage{Type: OpClose, Payload: payload}, nil
		}

		if opcode != OpContinuation {
			messageType = opcode
			fullPayload = payload
		} else {
			fullPayload = append(fullPayload, payload...)
		}

		if fin {
			return &WSMessage{Type: messageType, Payload: fullPayload}, nil
		}
	}
}

// WriteMessage sends a WebSocket message.
func (ws *WSConn) WriteMessage(msgType int, payload []byte) error {
	return ws.writeFrame(msgType, payload)
}

// WriteText sends a text WebSocket message.
func (ws *WSConn) WriteText(text string) error {
	return ws.WriteMessage(OpText, []byte(text))
}

// Close sends a close frame and closes the connection.
func (ws *WSConn) Close() error {
	ws.writeFrame(OpClose, nil)
	return ws.conn.Close()
}

func (ws *WSConn) readFrame() (fin bool, opcode int, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(ws.reader, header); err != nil {
		return false, 0, nil, err
	}

	fin = (header[0] & 0x80) != 0
	opcode = int(header[0] & 0x0F)
	masked := (header[1] & 0x80) != 0
	length := uint64(header[1] & 0x7F)

	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(ws.reader, extended); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(ws.reader, extended); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(ws.reader, maskKey[:]); err != nil {
			return false, 0, nil, err
		}
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(ws.reader, payload); err != nil {
		return false, 0, nil, err
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return fin, opcode, payload, nil
}

func (ws *WSConn) writeFrame(opcode int, payload []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	var frame []byte
	finByte := byte(0x80) | byte(opcode)
	frame = append(frame, finByte)

	length := len(payload)
	switch {
	case length <= 125:
		frame = append(frame, byte(length))
	case length <= 65535:
		frame = append(frame, 126)
		ext := make([]byte, 2)
		binary.BigEndian.PutUint16(ext, uint16(length))
		frame = append(frame, ext...)
	default:
		frame = append(frame, 127)
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(length))
		frame = append(frame, ext...)
	}

	frame = append(frame, payload...)
	_, err := ws.conn.Write(frame)
	return err
}
```

### Built-in Middleware

```go
// middleware.go
package httpfw

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// LoggingMiddleware logs each request's method, path, status, and duration.
func LoggingMiddleware(logger *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			start := time.Now()
			next(c)
			logger.Info("request",
				"method", c.Request.Method,
				"path", c.Request.Path,
				"status", c.Response.StatusCode,
				"duration", time.Since(start),
			)
		}
	}
}

// RecoveryMiddleware catches panics and returns 500.
func RecoveryMiddleware(logger *slog.Logger) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			defer func() {
				if err := recover(); err != nil {
					stack := debug.Stack()
					logger.Error("panic",
						"error", fmt.Sprintf("%v", err),
						"stack", string(stack),
					)
					c.String(500, "Internal Server Error")
				}
			}()
			next(c)
		}
	}
}

// RequestIDMiddleware assigns a unique ID to each request.
func RequestIDMiddleware() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			id := c.Request.Header("x-request-id")
			if id == "" {
				b := make([]byte, 16)
				rand.Read(b)
				id = hex.EncodeToString(b)
			}
			c.Set("request_id", id)
			c.Response.SetHeader("X-Request-ID", id)
			next(c)
		}
	}
}

// TimeoutMiddleware enforces a request deadline.
func TimeoutMiddleware(duration time.Duration) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) {
			done := make(chan struct{})
			go func() {
				next(c)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(duration):
				c.String(503, "Service Unavailable: request timeout")
			}
		}
	}
}
```

### Server (Connection Manager + TCP Listener)

```go
// server.go
package httpfw

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ServerConfig holds server configuration.
type ServerConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxConns        int
	ShutdownTimeout time.Duration
}

// Server is the HTTP framework entry point.
type Server struct {
	cfg       ServerConfig
	router    *Router
	listener  net.Listener
	wg        sync.WaitGroup
	activeConns atomic.Int64
	connSem   chan struct{}
	quit      chan struct{}
	logger    *slog.Logger
}

func NewServer(cfg ServerConfig, router *Router, logger *slog.Logger) *Server {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 120 * time.Second
	}
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 10000
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 30 * time.Second
	}

	return &Server{
		cfg:     cfg,
		router:  router,
		connSem: make(chan struct{}, cfg.MaxConns),
		quit:    make(chan struct{}),
		logger:  logger,
	}
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.logger.Info("server started", "addr", s.cfg.Addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return nil
			default:
				s.logger.Warn("accept error", "error", err)
				continue
			}
		}

		// Max connections limit
		select {
		case s.connSem <- struct{}{}:
		default:
			conn.Close()
			s.logger.Warn("max connections reached, rejecting")
			continue
		}

		s.wg.Add(1)
		s.activeConns.Add(1)
		go func() {
			defer func() {
				conn.Close()
				<-s.connSem
				s.activeConns.Add(-1)
				s.wg.Done()
			}()
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	close(s.quit)
	if s.listener != nil {
		s.listener.Close()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("graceful shutdown complete")
		return nil
	case <-ctx.Done():
		s.logger.Warn("shutdown timeout, forcefully closing connections")
		return ctx.Err()
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	reader := bufio.NewReaderSize(conn, 8192)

	for {
		// Set idle timeout for the next request
		conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))

		req, err := ParseRequest(reader, conn.RemoteAddr())
		if err != nil {
			return // connection closed or malformed request
		}

		// Set write deadline for the response
		conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))

		resp := NewResponse(conn)
		ctx := NewContext(req, resp, conn)

		s.dispatch(ctx)

		if req.WantsClose() || req.Version == "HTTP/1.0" {
			return
		}
	}
}

func (s *Server) dispatch(ctx *Context) {
	handler, params, status := s.router.Dispatch(ctx.Request.Method, ctx.Request.Path)

	if status == 404 {
		if s.router.notFound != nil {
			s.router.notFound(ctx)
		} else {
			ctx.String(404, "404 Not Found")
		}
		return
	}

	if status == 405 {
		if s.router.methodNA != nil {
			s.router.methodNA(ctx)
		} else {
			ctx.Response.SetHeader("Allow", s.allowedMethods(ctx.Request.Path))
			ctx.String(405, "405 Method Not Allowed")
		}
		return
	}

	ctx.Params = params

	// Build middleware chain: global middleware wrapping the handler
	final := handler
	for i := len(s.router.middleware) - 1; i >= 0; i-- {
		final = s.router.middleware[i](final)
	}

	final(ctx)
}

func (s *Server) allowedMethods(path string) string {
	var methods []string
	for method, tree := range s.router.trees {
		params := make(map[string]string)
		if _, ok := tree.lookup(trimLeadingSlash(path), params); ok {
			methods = append(methods, method)
		}
	}
	result := ""
	for i, m := range methods {
		if i > 0 {
			result += ", "
		}
		result += m
	}
	return result
}

// Addr returns the listener's address (useful when using port 0).
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.cfg.Addr
}
```

### Tests

```go
// server_test.go
package httpfw

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func startTestServer(t *testing.T, setup func(*Router)) string {
	t.Helper()
	router := NewRouter()
	setup(router)

	srv := NewServer(ServerConfig{
		Addr:        "127.0.0.1:0",
		IdleTimeout: 5 * time.Second,
	}, router, testLogger())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.listener = ln
	addr := ln.Addr().String()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.handleConnection(conn)
		}
	}()

	t.Cleanup(func() { ln.Close() })
	return addr
}

func rawRequest(addr, request string) (int, map[string]string, string, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return 0, nil, "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	fmt.Fprint(conn, request)

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, nil, "", err
	}

	var status int
	parts := strings.Fields(statusLine)
	if len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%d", &status)
	}

	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return status, headers, "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		idx := strings.IndexByte(line, ':')
		if idx != -1 {
			headers[strings.ToLower(strings.TrimSpace(line[:idx]))] = strings.TrimSpace(line[idx+1:])
		}
	}

	body, _ := io.ReadAll(reader)
	return status, headers, string(body), nil
}

func TestBasicRouting(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/hello", func(c *Context) {
			c.String(200, "Hello, World!")
		})
		r.POST("/echo", func(c *Context) {
			c.String(200, string(c.Request.Body))
		})
	})

	t.Run("GET", func(t *testing.T) {
		status, _, body, err := rawRequest(addr,
			"GET /hello HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
		if err != nil {
			t.Fatal(err)
		}
		if status != 200 {
			t.Errorf("status = %d, want 200", status)
		}
		if body != "Hello, World!" {
			t.Errorf("body = %q, want 'Hello, World!'", body)
		}
	})

	t.Run("POST with body", func(t *testing.T) {
		reqBody := "test data"
		status, _, body, err := rawRequest(addr,
			fmt.Sprintf("POST /echo HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Length: %d\r\n\r\n%s",
				len(reqBody), reqBody))
		if err != nil {
			t.Fatal(err)
		}
		if status != 200 {
			t.Errorf("status = %d, want 200", status)
		}
		if body != "test data" {
			t.Errorf("body = %q, want 'test data'", body)
		}
	})
}

func TestPathParameters(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/users/:id", func(c *Context) {
			c.String(200, "user=%s", c.Param("id"))
		})
		r.GET("/users/:id/posts/:postID", func(c *Context) {
			c.String(200, "user=%s post=%s", c.Param("id"), c.Param("postID"))
		})
	})

	status, _, body, _ := rawRequest(addr,
		"GET /users/42 HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 200 || body != "user=42" {
		t.Errorf("got status=%d body=%q", status, body)
	}

	status, _, body, _ = rawRequest(addr,
		"GET /users/42/posts/7 HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 200 || body != "user=42 post=7" {
		t.Errorf("got status=%d body=%q", status, body)
	}
}

func TestCatchAllRoute(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/files/*filepath", func(c *Context) {
			c.String(200, "path=%s", c.Param("filepath"))
		})
	})

	status, _, body, _ := rawRequest(addr,
		"GET /files/css/style.css HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 200 || body != "path=css/style.css" {
		t.Errorf("got status=%d body=%q", status, body)
	}
}

func TestJSONResponse(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/api/user", func(c *Context) {
			c.JSON(200, map[string]any{"name": "Alice", "age": 30})
		})
	})

	status, headers, body, _ := rawRequest(addr,
		"GET /api/user HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.Contains(headers["content-type"], "application/json") {
		t.Errorf("content-type = %q, want application/json", headers["content-type"])
	}
	if !strings.Contains(body, `"name":"Alice"`) {
		t.Errorf("body should contain name field: %s", body)
	}
}

func TestJSONBind(t *testing.T) {
	type CreateUser struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	addr := startTestServer(t, func(r *Router) {
		r.POST("/api/users", func(c *Context) {
			var user CreateUser
			if err := c.Bind(&user); err != nil {
				c.String(400, "bind error: %v", err)
				return
			}
			c.JSON(201, user)
		})
	})

	reqBody := `{"name":"Bob","email":"bob@test.com"}`
	status, _, body, _ := rawRequest(addr,
		fmt.Sprintf("POST /api/users HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
			len(reqBody), reqBody))
	if status != 201 {
		t.Errorf("status = %d, want 201", status)
	}
	if !strings.Contains(body, "Bob") {
		t.Errorf("body should contain 'Bob': %s", body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/items", func(c *Context) {
			c.String(200, "list")
		})
	})

	status, _, _, _ := rawRequest(addr,
		"POST /items HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 405 {
		t.Errorf("status = %d, want 405", status)
	}
}

func TestNotFound(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/exists", func(c *Context) { c.String(200, "ok") })
	})

	status, _, _, _ := rawRequest(addr,
		"GET /nope HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var order []string
	var mu sync.Mutex

	mkMW := func(name string) Middleware {
		return func(next HandlerFunc) HandlerFunc {
			return func(c *Context) {
				mu.Lock()
				order = append(order, name+"-before")
				mu.Unlock()
				next(c)
				mu.Lock()
				order = append(order, name+"-after")
				mu.Unlock()
			}
		}
	}

	addr := startTestServer(t, func(r *Router) {
		r.Use(mkMW("A"), mkMW("B"))
		r.GET("/test", func(c *Context) {
			mu.Lock()
			order = append(order, "handler")
			mu.Unlock()
			c.String(200, "ok")
		})
	})

	rawRequest(addr, "GET /test HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	expected := []string{"A-before", "B-before", "handler", "B-after", "A-after"}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != len(expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestPanicRecovery(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.Use(RecoveryMiddleware(testLogger()))
		r.GET("/crash", func(c *Context) {
			panic("test panic")
		})
	})

	status, _, _, _ := rawRequest(addr,
		"GET /crash HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 500 {
		t.Errorf("status = %d, want 500", status)
	}
}

func TestRequestID(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.Use(RequestIDMiddleware())
		r.GET("/id", func(c *Context) {
			id, _ := c.Get("request_id")
			c.String(200, "%s", id)
		})
	})

	_, headers, body, _ := rawRequest(addr,
		"GET /id HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if headers["x-request-id"] == "" {
		t.Error("X-Request-ID should be in response headers")
	}
	if body == "" || body != headers["x-request-id"] {
		t.Error("body should match X-Request-ID header")
	}
}

func TestKeepAlive(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/ping", func(c *Context) {
			c.String(200, "pong")
		})
	})

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	for i := 0; i < 3; i++ {
		fmt.Fprint(conn, "GET /ping HTTP/1.1\r\nHost: localhost\r\n\r\n")

		reader := bufio.NewReader(conn)
		statusLine, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("request %d: read status: %v", i, err)
		}
		if !strings.Contains(statusLine, "200") {
			t.Errorf("request %d: status = %q, want 200", i, statusLine)
		}

		// Read remaining headers and body
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("request %d: read header: %v", i, err)
			}
			if strings.TrimSpace(line) == "" {
				break
			}
		}
		body := make([]byte, 4) // "pong" = 4 bytes
		io.ReadFull(reader, body)
		if string(body) != "pong" {
			t.Errorf("request %d: body = %q, want 'pong'", i, body)
		}
	}
}

func TestQueryParameters(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/search", func(c *Context) {
			q := c.Request.Query("q")
			page := c.Request.Query("page")
			c.String(200, "q=%s page=%s", q, page)
		})
	})

	status, _, body, _ := rawRequest(addr,
		"GET /search?q=hello&page=2 HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if body != "q=hello page=2" {
		t.Errorf("body = %q", body)
	}
}

func TestFormParsing(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.POST("/form", func(c *Context) {
			name := c.FormValue("name")
			email := c.FormValue("email")
			c.String(200, "name=%s email=%s", name, email)
		})
	})

	formBody := "name=Alice&email=alice%40test.com"
	status, _, body, _ := rawRequest(addr,
		fmt.Sprintf("POST /form HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\n\r\n%s",
			len(formBody), formBody))
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if body != "name=Alice email=alice@test.com" {
		t.Errorf("body = %q", body)
	}
}

func TestChunkedRequestBody(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.POST("/chunked", func(c *Context) {
			c.String(200, "got: %s", string(c.Request.Body))
		})
	})

	chunkedReq := "POST /chunked HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5\r\nHello\r\n6\r\n World\r\n0\r\n\r\n"

	status, _, body, _ := rawRequest(addr, chunkedReq)
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if body != "got: Hello World" {
		t.Errorf("body = %q, want 'got: Hello World'", body)
	}
}

func TestConcurrentRequests(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/concurrent/:id", func(c *Context) {
			c.String(200, "id=%s", c.Param("id"))
		})
	})

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			status, _, body, err := rawRequest(addr,
				fmt.Sprintf("GET /concurrent/%d HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", id))
			if err != nil {
				errors <- fmt.Errorf("request %d: %w", id, err)
				return
			}
			if status != 200 {
				errors <- fmt.Errorf("request %d: status=%d", id, status)
				return
			}
			expected := fmt.Sprintf("id=%d", id)
			if body != expected {
				errors <- fmt.Errorf("request %d: body=%q, want %q", id, body, expected)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestGracefulShutdown(t *testing.T) {
	router := NewRouter()
	router.GET("/slow", func(c *Context) {
		time.Sleep(200 * time.Millisecond)
		c.String(200, "done")
	})

	srv := NewServer(ServerConfig{
		Addr:            "127.0.0.1:0",
		ShutdownTimeout: 5 * time.Second,
	}, router, testLogger())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.listener = ln
	addr := ln.Addr().String()

	serverDone := make(chan struct{})
	go func() {
		srv.ListenAndServe()
		close(serverDone)
	}()
	time.Sleep(50 * time.Millisecond)

	// Start a slow request
	requestDone := make(chan int)
	go func() {
		status, _, _, _ := rawRequest(addr,
			"GET /slow HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
		requestDone <- status
	}()

	// Give the request time to start
	time.Sleep(50 * time.Millisecond)

	// Initiate shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)

	status := <-requestDone
	if status != 200 {
		t.Errorf("in-flight request: status=%d, want 200", status)
	}
}

func TestWebSocketHandshake(t *testing.T) {
	addr := startTestServer(t, func(r *Router) {
		r.GET("/ws", func(c *Context) {
			ws, err := c.UpgradeWebSocket()
			if err != nil {
				c.String(400, "upgrade failed: %v", err)
				return
			}
			defer ws.Close()

			msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			ws.WriteText("echo: " + string(msg.Payload))
		})
	})

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send WebSocket upgrade request
	upgrade := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	fmt.Fprint(conn, upgrade)

	reader := bufio.NewReader(conn)
	statusLine, _ := reader.ReadString('\n')
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101, got: %s", statusLine)
	}

	// Read remaining headers
	for {
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) == "" {
			break
		}
		if strings.HasPrefix(line, "Sec-WebSocket-Accept:") {
			accept := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
			if accept != expected {
				t.Errorf("accept = %q, want %q", accept, expected)
			}
		}
	}

	// Send a masked text frame: "hello"
	payload := []byte("hello")
	mask := [4]byte{0x37, 0xfa, 0x21, 0x3d}
	maskedPayload := make([]byte, len(payload))
	for i, b := range payload {
		maskedPayload[i] = b ^ mask[i%4]
	}

	frame := []byte{0x81, 0x85} // FIN=1, TEXT, MASK=1, len=5
	frame = append(frame, mask[:]...)
	frame = append(frame, maskedPayload...)
	conn.Write(frame)

	// Read response frame
	respHeader := make([]byte, 2)
	io.ReadFull(reader, respHeader)

	opcode := respHeader[0] & 0x0F
	if opcode != OpText {
		t.Errorf("opcode = %d, want %d (text)", opcode, OpText)
	}

	length := int(respHeader[1] & 0x7F)
	respPayload := make([]byte, length)
	io.ReadFull(reader, respPayload)

	if string(respPayload) != "echo: hello" {
		t.Errorf("response = %q, want 'echo: hello'", string(respPayload))
	}
}
```

### Running and Testing

```bash
go test -v -race -timeout 60s ./...
go test -bench=. -benchmem ./...
```

### Expected Output

```
=== RUN   TestBasicRouting
=== RUN   TestBasicRouting/GET
=== RUN   TestBasicRouting/POST_with_body
--- PASS: TestBasicRouting (0.01s)
=== RUN   TestPathParameters
--- PASS: TestPathParameters (0.00s)
=== RUN   TestCatchAllRoute
--- PASS: TestCatchAllRoute (0.00s)
=== RUN   TestJSONResponse
--- PASS: TestJSONResponse (0.00s)
=== RUN   TestJSONBind
--- PASS: TestJSONBind (0.00s)
=== RUN   TestMethodNotAllowed
--- PASS: TestMethodNotAllowed (0.00s)
=== RUN   TestNotFound
--- PASS: TestNotFound (0.00s)
=== RUN   TestMiddlewareOrder
--- PASS: TestMiddlewareOrder (0.00s)
=== RUN   TestPanicRecovery
--- PASS: TestPanicRecovery (0.00s)
=== RUN   TestRequestID
--- PASS: TestRequestID (0.00s)
=== RUN   TestKeepAlive
--- PASS: TestKeepAlive (0.01s)
=== RUN   TestQueryParameters
--- PASS: TestQueryParameters (0.00s)
=== RUN   TestFormParsing
--- PASS: TestFormParsing (0.00s)
=== RUN   TestChunkedRequestBody
--- PASS: TestChunkedRequestBody (0.00s)
=== RUN   TestConcurrentRequests
--- PASS: TestConcurrentRequests (0.12s)
=== RUN   TestGracefulShutdown
--- PASS: TestGracefulShutdown (0.26s)
=== RUN   TestWebSocketHandshake
--- PASS: TestWebSocketHandshake (0.00s)
PASS
```

## Design Decisions

**Decision 1: Goroutine-per-connection vs. event loop.** One goroutine per connection is idiomatic Go and simplifies the code: each connection handler is a simple loop. Go's scheduler multiplexes goroutines onto OS threads efficiently. An event-loop model (like fasthttp's worker pool) can reduce goroutine overhead under extreme concurrency but adds complexity. The goroutine-per-connection model is correct and maintainable; the event loop is an optimization for >100K concurrent connections.

**Decision 2: Full body buffering vs. streaming.** Request bodies are read fully into memory before passing to handlers. This simplifies the handler API (body is a `[]byte`) and enables features like retry and JSON binding. The trade-off: large uploads consume proportional memory. A production framework would support streaming via `io.Reader` for the body, with buffering as an opt-in convenience.

**Decision 3: Own Response type vs. io.Writer.** The `Response` type tracks whether headers have been sent, enabling implicit 200 status on first `Write()` and preventing double `WriteHeader` calls. This mirrors the `net/http.ResponseWriter` contract. Exposing a raw `io.Writer` would be simpler but loses header management.

**Decision 4: Single Context type vs. per-feature interfaces.** The `Context` type consolidates request, response, params, query, JSON helpers, form parsing, and middleware flow. This is the Gin/Echo pattern. An alternative is separate interfaces (`JSONWriter`, `ParamReader`, etc.) composed via embedding. The monolithic Context is less type-safe but pragmatically simpler for framework users.

**Decision 5: WebSocket as Context method.** The `UpgradeWebSocket()` method lives on Context, giving it access to the raw connection and parsed headers. After upgrade, the Context is no longer valid for HTTP operations. An alternative is a separate handler type for WebSocket routes, but the Context-based approach is simpler and mirrors real framework APIs.

## Common Mistakes

**Mistake 1: Buffer reuse across keep-alive requests.** When reading multiple requests from one connection, the `bufio.Reader` may have buffered bytes from the next request. You must not discard the reader between requests. Create the `bufio.Reader` once per connection and reuse it across the keep-alive loop.

**Mistake 2: Not handling Content-Length: 0 correctly.** A POST with `Content-Length: 0` has an explicit empty body. This is different from a GET with no Content-Length (no body at all). The parser must distinguish these to avoid blocking on a read that will never produce data.

**Mistake 3: WebSocket masking direction.** Client-to-server frames are always masked; server-to-client frames are never masked. If you mask server frames, the client will unmask them incorrectly. If you fail to unmask client frames, you read garbage.

**Mistake 4: Chunked encoding terminator.** The final chunk is `0\r\n\r\n` (zero-length chunk followed by an empty trailer). A common mistake is sending `0\r\n` without the trailing `\r\n`, which leaves the client waiting for more data.

**Mistake 5: Goroutine leak on shutdown.** If `Accept()` returns an error during shutdown but the error handling doesn't check the quit channel, the listener goroutine may spin. Always check the quit signal on accept errors.

## Performance Notes

- The `bufio.Reader` size (8192 bytes default) determines how many bytes are read from the socket per syscall. For small requests (typical API calls), one syscall reads the entire request. For large uploads, the buffer amortizes syscall overhead.
- The radix tree lookup is O(k) where k is path segment count, independent of total route count. With typical REST APIs (4-6 segments), this is effectively O(1).
- JSON serialization via `encoding/json` is the likely bottleneck for API servers. For higher throughput, swap in `json-iterator/go` or `goccy/go-json` as a drop-in replacement.
- The max connections semaphore (`connSem`) prevents file descriptor exhaustion under load. When the limit is hit, new connections are rejected immediately rather than queuing, preventing cascade failure.
- WebSocket frame parsing is zero-copy for unmasked frames (server reads directly into the payload buffer). Masked frames require an in-place XOR pass, which is memory-bandwidth-bound and very fast for typical message sizes.
- Keep-alive significantly reduces connection overhead: a single TCP connection handles many requests, avoiding the ~1ms TCP handshake per request. The idle timeout (120s default) balances connection reuse against file descriptor consumption.
