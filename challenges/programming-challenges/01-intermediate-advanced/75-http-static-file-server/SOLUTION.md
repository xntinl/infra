# Solution: HTTP Static File Server

## Architecture Overview

The server has four layers: a **TCP listener** that accepts connections and reads raw HTTP requests, a **request parser** that extracts method, path, and headers from the byte stream, a **file resolver** that maps URL paths to filesystem paths with security validation, and a **response builder** that constructs HTTP responses with the correct headers for each scenario (full content, partial content, not modified, directory listing, error).

```
TCP Connection
    |
Request Parser (method, path, headers)
    |
Path Resolver (document root + security checks)
    |
    ├── Directory -> HTML listing
    ├── File exists, If-None-Match matches -> 304
    ├── File exists, Range header -> 206 Partial Content
    ├── File exists, Accept-Encoding: gzip -> compressed 200
    └── File exists -> 200 with body
```

MIME detection uses a static extension map. ETags combine file size and modification time. Gzip applies only to compressible types (text/*, application/json, application/javascript). Path traversal prevention resolves symlinks before checking the document root prefix.

## Go Solution

### Project Setup

```bash
mkdir -p static-server && cd static-server
go mod init static-server
```

### Implementation

```go
// server.go
package staticserver

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"html/template"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds server configuration.
type Config struct {
	DocRoot          string
	Addr             string
	EnableDirListing bool
	EnableGzip       bool
}

// Server serves static files over HTTP from raw TCP.
type Server struct {
	cfg      Config
	listener net.Listener
	wg       sync.WaitGroup
	quit     chan struct{}
}

func NewServer(cfg Config) (*Server, error) {
	absRoot, err := filepath.Abs(cfg.DocRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving doc root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving doc root symlinks: %w", err)
	}
	cfg.DocRoot = resolved

	return &Server{
		cfg:  cfg,
		quit: make(chan struct{}),
	}, nil
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.listener = ln

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return nil
			default:
				continue
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) Shutdown() {
	close(s.quit)
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

// --- HTTP Request Parsing ---

type httpRequest struct {
	method  string
	path    string
	version string
	headers map[string]string
}

func parseRequest(reader *bufio.Reader) (*httpRequest, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed request line: %q", line)
	}

	req := &httpRequest{
		method:  parts[0],
		path:    parts[1],
		version: parts[2],
		headers: make(map[string]string),
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
		key := strings.TrimSpace(header[:colonIdx])
		val := strings.TrimSpace(header[colonIdx+1:])
		req.headers[strings.ToLower(key)] = val
	}

	return req, nil
}

// --- MIME Types ---

var mimeTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".htm":  "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "application/javascript",
	".json": "application/json",
	".xml":  "application/xml",
	".txt":  "text/plain; charset=utf-8",
	".csv":  "text/csv",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".webp": "image/webp",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".pdf":  "application/pdf",
	".zip":  "application/zip",
	".wasm": "application/wasm",
}

func detectMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ct, ok := mimeTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

func isCompressible(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/javascript" ||
		contentType == "application/xml" ||
		contentType == "image/svg+xml" ||
		contentType == "application/wasm"
}

// --- Path Resolution and Security ---

func (s *Server) resolvePath(urlPath string) (string, error) {
	cleaned := filepath.Clean(urlPath)
	joined := filepath.Join(s.cfg.DocRoot, cleaned)

	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		return "", fmt.Errorf("path resolution failed: %w", err)
	}

	if !strings.HasPrefix(resolved, s.cfg.DocRoot) {
		return "", fmt.Errorf("path traversal blocked: %s escapes document root", urlPath)
	}

	return resolved, nil
}

// --- Range Request Parsing ---

type byteRange struct {
	start int64
	end   int64
}

func parseRange(header string, fileSize int64) (*byteRange, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return nil, fmt.Errorf("invalid range unit")
	}
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format")
	}

	var r byteRange

	if parts[0] == "" {
		// bytes=-N (last N bytes)
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, err
		}
		r.start = fileSize - suffix
		r.end = fileSize - 1
	} else if parts[1] == "" {
		// bytes=N- (from N to end)
		start, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, err
		}
		r.start = start
		r.end = fileSize - 1
	} else {
		// bytes=N-M
		start, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, err
		}
		end, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, err
		}
		r.start = start
		r.end = end
	}

	if r.start < 0 || r.end >= fileSize || r.start > r.end {
		return nil, fmt.Errorf("range not satisfiable")
	}

	return &r, nil
}

// --- ETag Generation ---

func generateETag(info os.FileInfo) string {
	return fmt.Sprintf(`"%x-%x"`, info.Size(), info.ModTime().UnixNano())
}

// --- Response Writing ---

func writeResponse(conn net.Conn, status int, headers map[string]string, body []byte) {
	statusText := map[int]string{
		200: "OK", 206: "Partial Content", 304: "Not Modified",
		400: "Bad Request", 403: "Forbidden", 404: "Not Found",
		405: "Method Not Allowed", 416: "Range Not Satisfiable",
		500: "Internal Server Error",
	}
	text := statusText[status]
	if text == "" {
		text = "Unknown"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, text))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123)))
	sb.WriteString("Connection: close\r\n")

	for k, v := range headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	if body != nil && headers["Content-Length"] == "" {
		sb.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))
	}

	sb.WriteString("\r\n")
	conn.Write([]byte(sb.String()))
	if body != nil {
		conn.Write(body)
	}
}

func writeError(conn net.Conn, status int) {
	statusText := map[int]string{
		400: "Bad Request", 403: "Forbidden", 404: "Not Found",
		405: "Method Not Allowed", 416: "Range Not Satisfiable",
		500: "Internal Server Error",
	}
	text := statusText[status]
	body := []byte(fmt.Sprintf("<h1>%d %s</h1>", status, text))
	writeResponse(conn, status, map[string]string{
		"Content-Type": "text/html; charset=utf-8",
	}, body)
}

// --- Directory Listing ---

var dirTemplate = template.Must(template.New("dir").Parse(`<!DOCTYPE html>
<html><head><title>Index of {{.Path}}</title>
<style>
body { font-family: monospace; margin: 2em; }
table { border-collapse: collapse; }
th, td { text-align: left; padding: 0.3em 1.5em; }
th { border-bottom: 1px solid #333; }
a { text-decoration: none; color: #0066cc; }
a:hover { text-decoration: underline; }
</style></head>
<body><h1>Index of {{.Path}}</h1>
<table>
<tr><th>Name</th><th>Size</th><th>Modified</th></tr>
{{if ne .Path "/"}}<tr><td><a href="../">../</a></td><td>-</td><td>-</td></tr>{{end}}
{{range .Entries}}<tr>
<td><a href="{{.Name}}{{if .IsDir}}/{{end}}">{{.Name}}{{if .IsDir}}/{{end}}</a></td>
<td>{{.SizeStr}}</td>
<td>{{.ModTime}}</td>
</tr>{{end}}
</table></body></html>`))

type dirData struct {
	Path    string
	Entries []dirEntry
}

type dirEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	SizeStr string
	ModTime string
}

func formatSize(size int64) string {
	switch {
	case size >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(size)/(1<<30))
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func renderDirListing(urlPath string, fsPath string) ([]byte, error) {
	entries, err := os.ReadDir(fsPath)
	if err != nil {
		return nil, err
	}

	data := dirData{Path: urlPath}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		de := dirEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		}
		if e.IsDir() {
			de.SizeStr = "-"
		} else {
			de.SizeStr = formatSize(info.Size())
		}
		data.Entries = append(data.Entries, de)
	}

	sort.Slice(data.Entries, func(i, j int) bool {
		if data.Entries[i].IsDir != data.Entries[j].IsDir {
			return data.Entries[i].IsDir
		}
		return data.Entries[i].Name < data.Entries[j].Name
	})

	var buf strings.Builder
	if err := dirTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// --- Connection Handler ---

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)
	req, err := parseRequest(reader)
	if err != nil {
		writeError(conn, 400)
		return
	}

	if req.method != "GET" && req.method != "HEAD" {
		writeResponse(conn, 405, map[string]string{
			"Allow":        "GET, HEAD",
			"Content-Type": "text/plain",
		}, []byte("Method Not Allowed"))
		return
	}

	fsPath, err := s.resolvePath(req.path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(conn, 404)
		} else {
			writeError(conn, 403)
		}
		return
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		writeError(conn, 404)
		return
	}

	if info.IsDir() {
		indexPath := filepath.Join(fsPath, "index.html")
		if indexInfo, err := os.Stat(indexPath); err == nil && !indexInfo.IsDir() {
			fsPath = indexPath
			info = indexInfo
		} else if s.cfg.EnableDirListing {
			listing, err := renderDirListing(req.path, fsPath)
			if err != nil {
				writeError(conn, 500)
				return
			}
			hdrs := map[string]string{"Content-Type": "text/html; charset=utf-8"}
			if req.method == "HEAD" {
				hdrs["Content-Length"] = strconv.Itoa(len(listing))
				writeResponse(conn, 200, hdrs, nil)
			} else {
				writeResponse(conn, 200, hdrs, listing)
			}
			return
		} else {
			writeError(conn, 403)
			return
		}
	}

	s.serveFile(conn, req, fsPath, info)
}

func (s *Server) serveFile(conn net.Conn, req *httpRequest, fsPath string, info os.FileInfo) {
	contentType := detectMIME(fsPath)
	etag := generateETag(info)

	// Conditional request: If-None-Match
	if clientETag := req.headers["if-none-match"]; clientETag == etag {
		writeResponse(conn, 304, map[string]string{"ETag": etag}, nil)
		return
	}

	file, err := os.Open(fsPath)
	if err != nil {
		writeError(conn, 500)
		return
	}
	defer file.Close()

	headers := map[string]string{
		"Content-Type":  contentType,
		"ETag":          etag,
		"Accept-Ranges": "bytes",
		"Cache-Control": "public, max-age=3600",
	}

	// Range request
	if rangeHeader, ok := req.headers["range"]; ok {
		r, err := parseRange(rangeHeader, info.Size())
		if err != nil {
			writeError(conn, 416)
			return
		}

		length := r.end - r.start + 1
		headers["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", r.start, r.end, info.Size())
		headers["Content-Length"] = strconv.FormatInt(length, 10)
		delete(headers, "Accept-Ranges")

		if req.method == "HEAD" {
			writeResponse(conn, 206, headers, nil)
			return
		}

		file.Seek(r.start, io.SeekStart)
		body := make([]byte, length)
		io.ReadFull(file, body)
		writeResponse(conn, 206, headers, body)
		return
	}

	// Full file response
	body, err := io.ReadAll(file)
	if err != nil {
		writeError(conn, 500)
		return
	}

	// Gzip compression
	useGzip := s.cfg.EnableGzip &&
		isCompressible(contentType) &&
		strings.Contains(req.headers["accept-encoding"], "gzip") &&
		len(body) > 1024 // don't compress tiny files

	if req.method == "HEAD" {
		headers["Content-Length"] = strconv.Itoa(len(body))
		writeResponse(conn, 200, headers, nil)
		return
	}

	if useGzip {
		var compressed strings.Builder
		gz, _ := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
		gz.Write(body)
		gz.Close()

		headers["Content-Encoding"] = "gzip"
		headers["Vary"] = "Accept-Encoding"
		compressedBytes := []byte(compressed.String())
		headers["Content-Length"] = strconv.Itoa(len(compressedBytes))
		writeResponse(conn, 200, headers, compressedBytes)
	} else {
		headers["Content-Length"] = strconv.Itoa(len(body))
		writeResponse(conn, 200, headers, body)
	}
}
```

### Tests

```go
// server_test.go
package staticserver

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<html><body>Hello</body></html>"), 0644)
	os.WriteFile(filepath.Join(dir, "style.css"),
		[]byte("body { margin: 0; }"), 0644)
	os.WriteFile(filepath.Join(dir, "app.js"),
		[]byte("console.log('hello');"), 0644)
	os.WriteFile(filepath.Join(dir, "data.json"),
		[]byte(`{"key":"value"}`), 0644)
	os.WriteFile(filepath.Join(dir, "image.png"),
		[]byte{0x89, 0x50, 0x4E, 0x47}, 0644)

	// Large text file for gzip testing
	largeText := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 100)
	os.WriteFile(filepath.Join(dir, "large.txt"), []byte(largeText), 0644)

	sub := filepath.Join(dir, "subdir")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "nested.txt"),
		[]byte("nested content"), 0644)

	return dir
}

func startTestServer(t *testing.T, dir string) string {
	t.Helper()
	srv, err := NewServer(Config{
		DocRoot:          dir,
		Addr:             "127.0.0.1:0",
		EnableDirListing: true,
		EnableGzip:       true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
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

	t.Cleanup(func() {
		ln.Close()
	})

	return addr
}

func sendRequest(addr, raw string) (status int, headers map[string]string, body string, err error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return 0, nil, "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	fmt.Fprint(conn, raw)

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, nil, "", err
	}
	parts := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
	if len(parts) < 2 {
		return 0, nil, "", fmt.Errorf("bad status line: %q", statusLine)
	}
	fmt.Sscanf(parts[1], "%d", &status)

	headers = make(map[string]string)
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

	bodyBytes, _ := io.ReadAll(reader)
	return status, headers, string(bodyBytes), nil
}

func TestServeStaticFile(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	status, headers, body, err := sendRequest(addr,
		"GET /index.html HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.Contains(headers["content-type"], "text/html") {
		t.Errorf("content-type = %q, want text/html", headers["content-type"])
	}
	if !strings.Contains(body, "Hello") {
		t.Errorf("body should contain 'Hello'")
	}
}

func TestMIMETypes(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	tests := []struct {
		path     string
		wantMIME string
	}{
		{"/style.css", "text/css"},
		{"/app.js", "application/javascript"},
		{"/data.json", "application/json"},
		{"/image.png", "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			_, headers, _, err := sendRequest(addr,
				fmt.Sprintf("GET %s HTTP/1.1\r\nHost: localhost\r\n\r\n", tt.path))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if !strings.Contains(headers["content-type"], tt.wantMIME) {
				t.Errorf("content-type = %q, want %q", headers["content-type"], tt.wantMIME)
			}
		})
	}
}

func TestDirectoryListing(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	status, _, body, err := sendRequest(addr,
		"GET /subdir/ HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "nested.txt") {
		t.Error("directory listing should contain nested.txt")
	}
}

func TestRangeRequest(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	status, headers, body, err := sendRequest(addr,
		"GET /large.txt HTTP/1.1\r\nHost: localhost\r\nRange: bytes=0-9\r\n\r\n")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if status != 206 {
		t.Errorf("status = %d, want 206", status)
	}
	if !strings.HasPrefix(headers["content-range"], "bytes 0-9/") {
		t.Errorf("content-range = %q, want bytes 0-9/...", headers["content-range"])
	}
	if len(body) != 10 {
		t.Errorf("body length = %d, want 10", len(body))
	}
}

func TestETagConditional(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	// First request to get the ETag
	_, headers, _, err := sendRequest(addr,
		"GET /index.html HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	etag := headers["etag"]
	if etag == "" {
		t.Fatal("ETag header should be present")
	}

	// Second request with If-None-Match
	status, _, _, err := sendRequest(addr,
		fmt.Sprintf("GET /index.html HTTP/1.1\r\nHost: localhost\r\nIf-None-Match: %s\r\n\r\n", etag))
	if err != nil {
		t.Fatalf("conditional request failed: %v", err)
	}
	if status != 304 {
		t.Errorf("status = %d, want 304", status)
	}
}

func TestPathTraversalBlocked(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	paths := []string{
		"/../../../etc/passwd",
		"/..%2F..%2Fetc/passwd",
		"/subdir/../../etc/passwd",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			status, _, _, err := sendRequest(addr,
				fmt.Sprintf("GET %s HTTP/1.1\r\nHost: localhost\r\n\r\n", p))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if status != 403 && status != 404 {
				t.Errorf("status = %d, want 403 or 404 for traversal attempt", status)
			}
		})
	}
}

func TestHeadRequest(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	status, headers, body, err := sendRequest(addr,
		"HEAD /index.html HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if headers["content-length"] == "" {
		t.Error("HEAD response should include Content-Length")
	}
	if body != "" {
		t.Error("HEAD response should have empty body")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	status, _, _, err := sendRequest(addr,
		"POST /index.html HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if status != 405 {
		t.Errorf("status = %d, want 405", status)
	}
}

func TestNotFound(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	status, _, _, err := sendRequest(addr,
		"GET /nonexistent.html HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestGzipCompression(t *testing.T) {
	dir := setupTestDir(t)
	addr := startTestServer(t, dir)

	_, headersGzip, _, err := sendRequest(addr,
		"GET /large.txt HTTP/1.1\r\nHost: localhost\r\nAccept-Encoding: gzip\r\n\r\n")
	if err != nil {
		t.Fatalf("gzip request failed: %v", err)
	}
	if headersGzip["content-encoding"] != "gzip" {
		t.Error("response should be gzip encoded")
	}

	_, headersPlain, _, err := sendRequest(addr,
		"GET /large.txt HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatalf("plain request failed: %v", err)
	}
	if headersPlain["content-encoding"] == "gzip" {
		t.Error("response without Accept-Encoding should not be gzip encoded")
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestServeStaticFile
--- PASS: TestServeStaticFile (0.00s)
=== RUN   TestMIMETypes
=== RUN   TestMIMETypes//style.css
=== RUN   TestMIMETypes//app.js
=== RUN   TestMIMETypes//data.json
=== RUN   TestMIMETypes//image.png
--- PASS: TestMIMETypes (0.01s)
=== RUN   TestDirectoryListing
--- PASS: TestDirectoryListing (0.00s)
=== RUN   TestRangeRequest
--- PASS: TestRangeRequest (0.00s)
=== RUN   TestETagConditional
--- PASS: TestETagConditional (0.00s)
=== RUN   TestPathTraversalBlocked
--- PASS: TestPathTraversalBlocked (0.01s)
=== RUN   TestHeadRequest
--- PASS: TestHeadRequest (0.00s)
=== RUN   TestMethodNotAllowed
--- PASS: TestMethodNotAllowed (0.00s)
=== RUN   TestNotFound
--- PASS: TestNotFound (0.00s)
=== RUN   TestGzipCompression
--- PASS: TestGzipCompression (0.00s)
PASS
```

## Design Decisions

**Decision 1: ETag from size + mtime, not content hash.** Hashing file content on every request is O(n) in file size and reads the entire file before the first response byte. Size + modification time provides the same semantic guarantee (ETag changes when file changes) at O(1) cost. The downside is that restoring a file from backup with the same content but a new mtime produces a new ETag, causing unnecessary retransmissions. For a static server, this is acceptable.

**Decision 2: Read entire file into memory vs. streaming.** The current implementation reads the full file into a `[]byte` before sending. This simplifies gzip compression (compress the whole buffer) and response construction (know Content-Length upfront). For very large files (multi-GB videos), this would be unacceptable -- you'd need to stream from the file descriptor and use chunked transfer encoding. This is a deliberate simplification.

**Decision 3: Gzip minimum threshold of 1KB.** Compressing tiny files wastes CPU and can actually increase the response size (gzip header overhead). The 1KB threshold skips compression for small files where the overhead is not justified. Production servers typically use 150-256 bytes as the threshold.

**Decision 4: Connection: close for simplicity.** Every response includes `Connection: close`, so each request uses one TCP connection. HTTP/1.1 keep-alive would amortize TCP handshake overhead but requires parsing Content-Length and managing connection state. This is omitted to focus on the file-serving mechanics.

## Common Mistakes

**Mistake 1: Path traversal via percent-encoded sequences.** Decoding `%2F` to `/` before cleaning the path can bypass `filepath.Clean`. Always clean the path after URL decoding, and always verify the final resolved path against the document root.

**Mistake 2: Range requests off-by-one.** The `Content-Range` header uses inclusive byte indices: `bytes 0-9/100` means bytes 0 through 9 (10 bytes total). A common mistake is returning `bytes 0-10/100` for 10 bytes, which is actually 11 bytes.

**Mistake 3: Not closing file descriptors on error paths.** If you open a file, then encounter an error (e.g., range not satisfiable), you must still close the file. Use `defer file.Close()` immediately after a successful open.

**Mistake 4: Gzip on images and already-compressed formats.** Compressing PNG, JPEG, ZIP, or MP4 files wastes CPU with no size reduction (they're already compressed). Only compress text-based formats.

## Performance Notes

- The MIME type map lookup is O(1) per request. For the extension extraction, `filepath.Ext` does a single backward scan of the filename.
- Reading the entire file into memory before responding means max memory usage is proportional to the largest file being served. Under concurrent requests, this multiplies. A production server would use `io.Copy` with `sendfile(2)` syscall for zero-copy serving.
- Gzip at `BestSpeed` level adds ~0.5ms per MB of content. For repeated requests to the same file, consider caching the compressed version on disk or in memory.
- The ETag check avoids reading the file entirely for conditional requests, making 304 responses essentially free (just a `stat` syscall).
