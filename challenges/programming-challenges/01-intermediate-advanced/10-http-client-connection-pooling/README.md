# 10. HTTP/1.1 Client with Connection Pooling

<!--
difficulty: intermediate-advanced
category: caching-and-networking
languages: [go]
concepts: [http-protocol, tcp-connections, connection-pooling, keep-alive, chunked-encoding, pipelining]
estimated_time: 5-6 hours
bloom_level: analyze
prerequisites: [go-basics, tcp-networking, http-fundamentals, goroutines, channels, sync-primitives]
-->

## Languages

- Go (1.22+)

## Prerequisites

- TCP socket programming with `net.Conn` (Dial, Read, Write, Close)
- HTTP/1.1 protocol basics (request/response format, headers, status codes)
- Goroutines, channels, and `sync.Mutex` for concurrent connection management
- Buffered I/O with `bufio.Reader` and `bufio.Writer`
- Understanding of keep-alive connections and why connection reuse matters

## Learning Objectives

- **Implement** HTTP/1.1 request serialization and response parsing on raw TCP connections
- **Design** a per-host connection pool with configurable limits and idle timeout management
- **Analyze** the performance impact of connection reuse versus connect-per-request under concurrent load
- **Apply** chunked transfer encoding for responses without a Content-Length header
- **Evaluate** connection health through idle timeouts and pre-request validation

## The Challenge

Go's `net/http` package is one of the best HTTP clients in any language. It handles connection pooling, TLS, redirects, cookies, and a dozen edge cases transparently. But that transparency means most Go developers have never seen what actually happens when you make an HTTP request: a TCP connection opens, bytes flow in a specific format, and the connection either closes or stays alive for reuse.

Your task is to build an HTTP/1.1 client library from scratch, using only `net.Conn` for transport. No `net/http` allowed. You will format HTTP requests as raw bytes, parse HTTP responses character by character, manage a pool of persistent connections organized by host, and handle the nuances that make HTTP/1.1 work in practice: keep-alive negotiation, chunked transfer encoding, idle connection cleanup, and automatic redirect following.

This challenge forces you to understand every byte of the HTTP protocol. When you finish, you will know exactly what `http.Client` does for you and why connection pooling is the single biggest performance optimization in HTTP client libraries.

## Requirements

1. Implement request serialization: format `Method SP Request-URI SP HTTP/1.1 CRLF Headers CRLF Body` as bytes and write to a `net.Conn`
2. Implement response parsing: read the status line, parse headers (handle multi-line header values), and read the body based on Content-Length or chunked transfer encoding
3. Parse chunked transfer encoding: read chunk-size (hex), CRLF, chunk-data, CRLF in a loop until a zero-length chunk signals the end
4. Maintain a per-host connection pool: reuse idle connections for subsequent requests to the same `host:port`
5. Enforce per-host connection limits (default: 6 connections per host). When the limit is reached, queue the request until a connection becomes available
6. Implement idle connection timeout: close connections that have been idle longer than a configurable duration (default: 90 seconds)
7. Add connection health checking: before reusing an idle connection, verify it has not been closed by the server (peek for EOF or RST)
8. Follow HTTP redirects (301, 302, 307, 308) up to a configurable maximum (default: 10). Preserve method and body for 307/308
9. Set `Connection: keep-alive` and `Host` headers automatically. Parse `Connection: close` from responses to mark connections as non-reusable
10. Provide a clean API: `client.Get(url)`, `client.Post(url, contentType, body)` returning a `Response` struct with status, headers, and body

## Hints

<details>
<summary>Hint 1: Connection pool structure</summary>

Organize idle connections by host key (`host:port`). A channel per host works well as a bounded pool:

```go
type ConnPool struct {
    mu       sync.Mutex
    idle     map[string][]*persistConn // host:port -> idle conns
    maxIdle  int
    maxPerHost int
    idleTimeout time.Duration
}

type persistConn struct {
    conn      net.Conn
    br        *bufio.Reader
    bw        *bufio.Writer
    host      string
    idleSince time.Time
    reusable  bool
}
```
</details>

<details>
<summary>Hint 2: Response parsing with bufio.Reader</summary>

Use `bufio.Reader` to parse the response line by line, then switch to byte-counted reads for the body:

```go
func parseResponse(br *bufio.Reader) (*Response, error) {
    // Status line: "HTTP/1.1 200 OK\r\n"
    statusLine, err := br.ReadString('\n')
    // Parse headers until empty line
    headers := make(http.Header)
    for {
        line, err := br.ReadString('\n')
        line = strings.TrimRight(line, "\r\n")
        if line == "" {
            break
        }
        key, value, _ := strings.Cut(line, ": ")
        headers.Add(key, value)
    }
    // Read body based on Transfer-Encoding or Content-Length
}
```
</details>

<details>
<summary>Hint 3: Chunked transfer decoding</summary>

Each chunk is: size in hex, CRLF, data bytes, CRLF. The final chunk has size 0:

```go
func readChunked(br *bufio.Reader) ([]byte, error) {
    var body []byte
    for {
        sizeLine, _ := br.ReadString('\n')
        size, _ := strconv.ParseInt(strings.TrimSpace(sizeLine), 16, 64)
        if size == 0 {
            br.ReadString('\n') // trailing CRLF
            break
        }
        chunk := make([]byte, size)
        io.ReadFull(br, chunk)
        br.ReadString('\n') // trailing CRLF
        body = append(body, chunk...)
    }
    return body, nil
}
```
</details>

<details>
<summary>Hint 4: Connection health check before reuse</summary>

Before reusing an idle connection, check if the server closed it:

```go
func (pc *persistConn) isAlive() bool {
    pc.conn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
    _, err := pc.br.Peek(1)
    pc.conn.SetReadDeadline(time.Time{}) // reset
    if err == nil {
        return false // unexpected data means server sent something (probably close)
    }
    if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
        return true // timeout means no data = connection still open
    }
    return false // any other error means dead
}
```
</details>

## Acceptance Criteria

- [ ] Sends well-formed HTTP/1.1 requests with correct Host and Content-Length headers
- [ ] Parses HTTP responses including status code, headers, and body
- [ ] Correctly decodes chunked transfer-encoded responses
- [ ] Reuses connections for consecutive requests to the same host
- [ ] Enforces per-host connection limit (queues excess requests)
- [ ] Closes idle connections after the configured timeout
- [ ] Validates connection health before reuse (detects server-side close)
- [ ] Follows 301/302/307/308 redirects up to the configured maximum
- [ ] Handles `Connection: close` responses by marking connections non-reusable
- [ ] 50 concurrent requests to the same host complete using at most 6 connections

## Research Resources

- [RFC 9112: HTTP/1.1 Message Syntax and Routing](https://www.rfc-editor.org/rfc/rfc9112) -- the current HTTP/1.1 specification for message format
- [RFC 9110: HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110) -- redirect behavior, method semantics, connection management
- [Go net package: Conn interface](https://pkg.go.dev/net#Conn) -- the raw TCP connection API you will build on
- [Go net/http Transport source](https://github.com/golang/go/blob/master/src/net/http/transport.go) -- study the production implementation for connection pooling patterns
- [MDN: Transfer-Encoding](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Transfer-Encoding) -- chunked encoding explained with examples
- [High Performance Browser Networking: HTTP/1.1](https://hpbn.co/http1x/) -- why connection reuse is the most important HTTP optimization
