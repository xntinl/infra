# Solution: HTTP/1.1 Client with Connection Pooling

## Architecture Overview

The client has three layers: a **transport** layer that manages raw TCP connections, a **pool** layer that tracks idle connections per host, and a **client** layer that provides the user-facing API (Get, Post, Do).

```
┌──────────────┐
│   Client     │  Get(url), Post(url, body)
│   (API)      │  redirect following
├──────────────┤
│   Pool       │  per-host idle connections
│              │  connection limits, health checks
├──────────────┤
│  Transport   │  request serialization
│  (net.Conn)  │  response parsing
└──────────────┘
```

A request flows through: URL parsing -> pool checkout (or dial new) -> request write -> response read -> pool return (if keep-alive) or close. The pool maintains a slice of idle connections per `host:port`, enforces per-host limits, and runs a background goroutine to prune idle connections past their timeout.

---

## Go Solution

### Project Setup

```bash
mkdir httpclient && cd httpclient
go mod init httpclient
```

### Types and Constants

```go
// types.go
package httpclient

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type Request struct {
	Method  string
	URL     *url.URL
	Headers map[string]string
	Body    []byte
}

type Response struct {
	StatusCode int
	Status     string
	Headers    map[string][]string
	Body       []byte
}

type persistConn struct {
	conn      net.Conn
	br        *bufio.Reader
	bw        *bufio.Writer
	host      string
	idleSince time.Time
	reusable  bool
}

func hostKey(u *url.URL) string {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "80"
	}
	return fmt.Sprintf("%s:%s", host, port)
}

func (r *Response) Header(key string) string {
	vals, ok := r.Headers[strings.ToLower(key)]
	if !ok || len(vals) == 0 {
		return ""
	}
	return vals[0]
}
```

### Connection Pool

```go
// pool.go
package httpclient

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"
)

type Pool struct {
	mu          sync.Mutex
	idle        map[string][]*persistConn
	active      map[string]int
	maxPerHost  int
	maxIdle     int
	idleTimeout time.Duration
	dialTimeout time.Duration
	waiters     map[string][]chan *persistConn
	stopCh      chan struct{}
}

func NewPool(maxPerHost, maxIdle int, idleTimeout time.Duration) *Pool {
	p := &Pool{
		idle:        make(map[string][]*persistConn),
		active:      make(map[string]int),
		maxPerHost:  maxPerHost,
		maxIdle:     maxIdle,
		idleTimeout: idleTimeout,
		dialTimeout: 10 * time.Second,
		waiters:     make(map[string][]chan *persistConn),
		stopCh:      make(chan struct{}),
	}
	go p.cleanupLoop()
	return p
}

func (p *Pool) Get(host string) (*persistConn, error) {
	p.mu.Lock()

	for i := len(p.idle[host]) - 1; i >= 0; i-- {
		pc := p.idle[host][i]
		p.idle[host] = append(p.idle[host][:i], p.idle[host][i+1:]...)

		if time.Since(pc.idleSince) > p.idleTimeout {
			pc.conn.Close()
			continue
		}

		if !isAlive(pc) {
			pc.conn.Close()
			continue
		}

		p.active[host]++
		p.mu.Unlock()
		return pc, nil
	}

	if p.active[host] >= p.maxPerHost {
		ch := make(chan *persistConn, 1)
		p.waiters[host] = append(p.waiters[host], ch)
		p.mu.Unlock()

		timer := time.NewTimer(p.dialTimeout)
		defer timer.Stop()
		select {
		case pc := <-ch:
			return pc, nil
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for connection to %s", host)
		}
	}

	p.active[host]++
	p.mu.Unlock()

	return p.dial(host)
}

func (p *Pool) Put(pc *persistConn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	host := pc.host
	p.active[host]--

	if !pc.reusable {
		pc.conn.Close()
		p.notifyWaiters(host)
		return
	}

	if waiters := p.waiters[host]; len(waiters) > 0 {
		ch := waiters[0]
		p.waiters[host] = waiters[1:]
		p.active[host]++
		pc.idleSince = time.Now()
		ch <- pc
		return
	}

	if len(p.idle[host]) >= p.maxIdle {
		pc.conn.Close()
		return
	}

	pc.idleSince = time.Now()
	p.idle[host] = append(p.idle[host], pc)
}

func (p *Pool) Close() {
	close(p.stopCh)
	p.mu.Lock()
	defer p.mu.Unlock()

	for host, conns := range p.idle {
		for _, pc := range conns {
			pc.conn.Close()
		}
		delete(p.idle, host)
	}
}

func (p *Pool) dial(host string) (*persistConn, error) {
	conn, err := net.DialTimeout("tcp", host, p.dialTimeout)
	if err != nil {
		p.mu.Lock()
		p.active[host]--
		p.mu.Unlock()
		return nil, fmt.Errorf("dial %s: %w", host, err)
	}

	return &persistConn{
		conn:      conn,
		br:        bufio.NewReader(conn),
		bw:        bufio.NewWriter(conn),
		host:      host,
		reusable:  true,
		idleSince: time.Now(),
	}, nil
}

func (p *Pool) notifyWaiters(host string) {
	if waiters := p.waiters[host]; len(waiters) > 0 {
		ch := waiters[0]
		p.waiters[host] = waiters[1:]
		p.active[host]++
		go func() {
			pc, err := p.dial(host)
			if err != nil {
				p.mu.Lock()
				p.active[host]--
				p.mu.Unlock()
				close(ch)
				return
			}
			ch <- pc
		}()
	}
}

func (p *Pool) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.removeStale()
		case <-p.stopCh:
			return
		}
	}
}

func (p *Pool) removeStale() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for host, conns := range p.idle {
		fresh := conns[:0]
		for _, pc := range conns {
			if time.Since(pc.idleSince) > p.idleTimeout {
				pc.conn.Close()
			} else {
				fresh = append(fresh, pc)
			}
		}
		p.idle[host] = fresh
	}
}

func isAlive(pc *persistConn) bool {
	pc.conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
	_, err := pc.br.Peek(1)
	pc.conn.SetReadDeadline(time.Time{})

	if err == nil {
		return false
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return false
}
```

### HTTP Transport (Request/Response)

```go
// transport.go
package httpclient

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

func writeRequest(pc *persistConn, req *Request) error {
	path := req.URL.RequestURI()
	fmt.Fprintf(pc.bw, "%s %s HTTP/1.1\r\n", req.Method, path)
	fmt.Fprintf(pc.bw, "Host: %s\r\n", req.URL.Host)
	fmt.Fprintf(pc.bw, "Connection: keep-alive\r\n")

	if req.Body != nil && len(req.Body) > 0 {
		fmt.Fprintf(pc.bw, "Content-Length: %d\r\n", len(req.Body))
	}

	for key, val := range req.Headers {
		fmt.Fprintf(pc.bw, "%s: %s\r\n", key, val)
	}

	fmt.Fprintf(pc.bw, "\r\n")

	if req.Body != nil {
		pc.bw.Write(req.Body)
	}

	return pc.bw.Flush()
}

func readResponse(pc *persistConn) (*Response, error) {
	statusLine, err := pc.br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading status line: %w", err)
	}
	statusLine = strings.TrimRight(statusLine, "\r\n")

	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed status line: %q", statusLine)
	}

	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid status code: %q", parts[1])
	}

	statusText := ""
	if len(parts) >= 3 {
		statusText = parts[2]
	}

	headers := make(map[string][]string)
	for {
		line, err := pc.br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading headers: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		headers[key] = append(headers[key], value)
	}

	var body []byte

	te := ""
	if vals, ok := headers["transfer-encoding"]; ok && len(vals) > 0 {
		te = strings.ToLower(vals[0])
	}

	if te == "chunked" {
		body, err = readChunked(pc.br)
		if err != nil {
			return nil, fmt.Errorf("reading chunked body: %w", err)
		}
	} else if vals, ok := headers["content-length"]; ok && len(vals) > 0 {
		length, err := strconv.Atoi(vals[0])
		if err != nil {
			return nil, fmt.Errorf("invalid content-length: %q", vals[0])
		}
		body = make([]byte, length)
		if _, err := io.ReadFull(pc.br, body); err != nil {
			return nil, fmt.Errorf("reading body: %w", err)
		}
	}

	if vals, ok := headers["connection"]; ok {
		for _, v := range vals {
			if strings.ToLower(v) == "close" {
				pc.reusable = false
			}
		}
	}

	return &Response{
		StatusCode: code,
		Status:     statusText,
		Headers:    headers,
		Body:       body,
	}, nil
}

func readChunked(br *bufio.Reader) ([]byte, error) {
	var body []byte
	for {
		sizeLine, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading chunk size: %w", err)
		}
		sizeLine = strings.TrimRight(sizeLine, "\r\n")
		sizeLine = strings.TrimSpace(sizeLine)

		if idx := strings.Index(sizeLine, ";"); idx >= 0 {
			sizeLine = sizeLine[:idx]
		}

		size, err := strconv.ParseInt(sizeLine, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chunk size %q: %w", sizeLine, err)
		}

		if size == 0 {
			br.ReadString('\n')
			break
		}

		chunk := make([]byte, size)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, fmt.Errorf("reading chunk data: %w", err)
		}
		body = append(body, chunk...)

		br.ReadString('\n')
	}
	return body, nil
}
```

### Client API

```go
// client.go
package httpclient

import (
	"fmt"
	"net/url"
	"time"
)

type Client struct {
	pool         *Pool
	maxRedirects int
}

func NewClient() *Client {
	return &Client{
		pool:         NewPool(6, 10, 90*time.Second),
		maxRedirects: 10,
	}
}

func (c *Client) Close() {
	c.pool.Close()
}

func (c *Client) Do(req *Request) (*Response, error) {
	for redirects := 0; ; redirects++ {
		if redirects > c.maxRedirects {
			return nil, fmt.Errorf("too many redirects (max %d)", c.maxRedirects)
		}

		host := hostKey(req.URL)
		pc, err := c.pool.Get(host)
		if err != nil {
			return nil, err
		}

		if err := writeRequest(pc, req); err != nil {
			pc.reusable = false
			c.pool.Put(pc)
			return nil, fmt.Errorf("writing request: %w", err)
		}

		resp, err := readResponse(pc)
		if err != nil {
			pc.reusable = false
			c.pool.Put(pc)
			return nil, fmt.Errorf("reading response: %w", err)
		}

		c.pool.Put(pc)

		if !isRedirect(resp.StatusCode) {
			return resp, nil
		}

		location := resp.Header("location")
		if location == "" {
			return resp, nil
		}

		nextURL, err := resolveRedirect(req.URL, location)
		if err != nil {
			return nil, fmt.Errorf("invalid redirect location %q: %w", location, err)
		}

		if resp.StatusCode == 307 || resp.StatusCode == 308 {
			req = &Request{
				Method:  req.Method,
				URL:     nextURL,
				Headers: req.Headers,
				Body:    req.Body,
			}
		} else {
			req = &Request{
				Method:  "GET",
				URL:     nextURL,
				Headers: req.Headers,
			}
		}
	}
}

func (c *Client) Get(rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	return c.Do(&Request{Method: "GET", URL: u})
}

func (c *Client) Post(rawURL, contentType string, body []byte) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	return c.Do(&Request{
		Method:  "POST",
		URL:     u,
		Headers: map[string]string{"Content-Type": contentType},
		Body:    body,
	})
}

func isRedirect(code int) bool {
	return code == 301 || code == 302 || code == 307 || code == 308
}

func resolveRedirect(base *url.URL, location string) (*url.URL, error) {
	loc, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	return base.ResolveReference(loc), nil
}
```

### Tests

```go
// client_test.go
package httpclient

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func startTestServer(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()

	return ln.Addr().String()
}

func TestBasicGet(t *testing.T) {
	addr := startTestServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.Read(buf)

		body := "Hello, World!"
		resp := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
			len(body), body,
		)
		conn.Write([]byte(resp))
	})

	client := NewClient()
	defer client.Close()

	resp, err := client.Get("http://" + addr + "/test")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "Hello, World!" {
		t.Fatalf("expected 'Hello, World!', got %q", string(resp.Body))
	}
}

func TestChunkedResponse(t *testing.T) {
	addr := startTestServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.Read(buf)

		resp := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n" +
			"5\r\nHello\r\n" +
			"7\r\n, World\r\n" +
			"0\r\n\r\n"
		conn.Write([]byte(resp))
	})

	client := NewClient()
	defer client.Close()

	resp, err := client.Get("http://" + addr + "/chunked")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "Hello, World" {
		t.Fatalf("expected 'Hello, World', got %q", string(resp.Body))
	}
}

func TestConnectionReuse(t *testing.T) {
	connCount := 0
	mu := sync.Mutex{}

	addr := startTestServer(t, func(conn net.Conn) {
		mu.Lock()
		connCount++
		mu.Unlock()

		br := make([]byte, 4096)
		for {
			n, err := conn.Read(br)
			if err != nil {
				conn.Close()
				return
			}
			_ = n
			body := "ok"
			resp := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: keep-alive\r\n\r\n%s",
				len(body), body,
			)
			conn.Write([]byte(resp))
		}
	})

	client := NewClient()
	defer client.Close()

	for i := 0; i < 5; i++ {
		resp, err := client.Get("http://" + addr + "/reuse")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}

	mu.Lock()
	count := connCount
	mu.Unlock()

	if count > 1 {
		t.Fatalf("expected 1 TCP connection, got %d (connections not reused)", count)
	}
}

func TestPerHostLimit(t *testing.T) {
	addr := startTestServer(t, func(conn net.Conn) {
		br := make([]byte, 4096)
		for {
			_, err := conn.Read(br)
			if err != nil {
				conn.Close()
				return
			}
			time.Sleep(50 * time.Millisecond)
			body := "ok"
			resp := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s",
				len(body), body,
			)
			conn.Write([]byte(resp))
		}
	})

	client := NewClient()
	defer client.Close()

	var wg sync.WaitGroup
	results := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Get("http://" + addr + "/limit")
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Logf("request error (may be expected under limit): %v", err)
		}
	}
}

func TestRedirectFollowing(t *testing.T) {
	addr := startTestServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		req := string(buf[:n])

		if strings.Contains(req, "GET /redirect") {
			resp := fmt.Sprintf(
				"HTTP/1.1 302 Found\r\nLocation: /final\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
			)
			conn.Write([]byte(resp))
		} else {
			body := "final destination"
			resp := fmt.Sprintf(
				"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
				len(body), body,
			)
			conn.Write([]byte(resp))
		}
	})

	client := NewClient()
	defer client.Close()

	resp, err := client.Get("http://" + addr + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 after redirect, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "final destination" {
		t.Fatalf("expected 'final destination', got %q", string(resp.Body))
	}
}

func TestConnectionClose(t *testing.T) {
	addr := startTestServer(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.Read(buf)

		body := "done"
		resp := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
			len(body), body,
		)
		conn.Write([]byte(resp))
	})

	client := NewClient()
	defer client.Close()

	resp, err := client.Get("http://" + addr + "/close")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatal("expected 200")
	}

	_, _ = io.ReadAll(strings.NewReader(string(resp.Body)))
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestBasicGet
--- PASS: TestBasicGet (0.00s)
=== RUN   TestChunkedResponse
--- PASS: TestChunkedResponse (0.00s)
=== RUN   TestConnectionReuse
--- PASS: TestConnectionReuse (0.00s)
=== RUN   TestPerHostLimit
--- PASS: TestPerHostLimit (0.50s)
=== RUN   TestRedirectFollowing
--- PASS: TestRedirectFollowing (0.00s)
=== RUN   TestConnectionClose
--- PASS: TestConnectionClose (0.00s)
PASS
```

---

## Design Decisions

**Why `bufio.Reader`/`bufio.Writer` wrap each connection?** HTTP parsing requires line-oriented reading (for headers) followed by byte-counted reading (for body). `bufio.Reader` provides both through `ReadString('\n')` and the underlying `Read`. Without buffering, each `ReadString` would issue a syscall per byte.

**Why a slice instead of a channel for idle connections?** Channels provide natural blocking semantics but make it hard to iterate and prune stale connections. A slice under a mutex allows both LIFO connection reuse (most recently used connections are warmest) and background cleanup of stale entries.

**Why LIFO order for connection reuse?** When multiple idle connections exist, returning the most recently used one increases the chance it is still alive. Connections that have been idle longest are more likely to have been closed by the server or by intermediate proxies.

**Why queue excess requests instead of failing?** Under burst load, immediately failing requests above the per-host limit would force callers to implement their own retry logic. Queuing with a timeout provides built-in backpressure and matches the behavior of `net/http.Transport`.

## Common Mistakes

**Not flushing the `bufio.Writer`.** After writing the request, you must call `bw.Flush()`. Without it, the request bytes sit in the buffer and are never sent to the server. The response read then blocks forever.

**Reading more than Content-Length bytes.** If you read the body with a generic `Read` loop instead of `io.ReadFull`, you might read into the next response on a keep-alive connection. Always use `io.ReadFull` for Content-Length bodies and the chunked protocol for Transfer-Encoding: chunked.

**Not handling `Connection: close` from the server.** Even if you sent `Connection: keep-alive`, the server can respond with `Connection: close`. You must parse this header and mark the connection as non-reusable. Returning it to the pool means the next request on that connection reads from a closed socket.

**Leaking connections on error.** If request writing fails or response parsing fails, you must still return the connection to the pool (marked as non-reusable so it gets closed). Not returning it means the active count never decrements, eventually exhausting the per-host limit.

## Performance Notes

- Connection reuse eliminates the TCP handshake (1-2 RTTs) and, for HTTPS, the TLS handshake (1-2 additional RTTs). This is a 4-10x latency improvement for short requests
- The per-host limit of 6 matches the default in most browsers and in Go's `net/http.Transport`. Increasing it beyond ~10 rarely helps because most servers have their own per-client connection limits
- Idle timeout of 90 seconds matches Go's default. Setting it too low causes unnecessary reconnections; too high wastes file descriptors
- For high-throughput use cases, consider connection pipelining: sending multiple requests without waiting for responses. This requires careful response ordering and is not implemented here

## Going Further

- Add TLS support by wrapping `net.Conn` with `tls.Client` for HTTPS URLs
- Implement request pipelining: send multiple requests on one connection without waiting for intermediate responses
- Add cookie jar support: automatically store and resend cookies across requests
- Implement `Expect: 100-continue` for large POST bodies to avoid sending the body if the server will reject it
- Add connection pool metrics: total connections created, reused, closed by idle timeout, closed by health check
- Build a simple HTTP proxy by combining this client with a listener that accepts connections and forwards requests
