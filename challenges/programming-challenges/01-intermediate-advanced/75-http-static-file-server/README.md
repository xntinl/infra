# 75. HTTP Static File Server

<!--
difficulty: intermediate-advanced
category: web-servers-and-http
languages: [go]
concepts: [file-io, mime-types, range-requests, etag, gzip-encoding, directory-listing, path-traversal-prevention]
estimated_time: 4-5 hours
bloom_level: apply
prerequisites: [go-basics, file-io, http-headers, content-negotiation, hashing]
-->

## Languages

- Go (1.22+)

## Prerequisites

- File I/O in Go (`os.Open`, `os.Stat`, `io.ReadSeeker`)
- HTTP response headers: `Content-Type`, `Content-Length`, `Accept-Encoding`, `Range`, `ETag`, `If-None-Match`
- Basic understanding of MIME types and content negotiation
- Hashing for ETag generation (`crypto/sha256` or similar)
- Path manipulation and security implications of directory traversal

## Learning Objectives

- **Implement** a static file server that resolves URL paths to filesystem paths securely
- **Apply** HTTP range requests to support partial content delivery (video seeking, resume)
- **Design** an ETag-based conditional request system that avoids redundant transfers
- **Implement** transparent gzip compression based on `Accept-Encoding` negotiation
- **Analyze** path traversal attack vectors and implement robust prevention

## The Challenge

Serving static files seems trivial until you hit the edge cases. A browser requests a video and expects to seek to the middle -- your server needs range request support. A client revisits a page and sends `If-None-Match` -- your server should return 304 Not Modified instead of retransmitting. A mobile client on a slow connection benefits from gzip compression. And one malicious request with `../../../etc/passwd` should never escape your document root.

Your task is to build a static file server from raw TCP connections (using `net.Conn`, not `net/http.FileServer`). The server must correctly detect MIME types, generate HTML directory listings, support HTTP range requests for partial content, implement ETag-based conditional requests, compress responses with gzip when the client supports it, and prevent path traversal attacks. You will parse HTTP/1.1 request lines and headers yourself and construct proper HTTP responses.

## Requirements

1. Accept TCP connections and parse HTTP/1.1 requests (method, path, headers) from raw bytes
2. Resolve the URL path to a filesystem path within a configurable document root directory
3. Detect MIME types using file extension mapping (at minimum: `.html`, `.css`, `.js`, `.json`, `.png`, `.jpg`, `.gif`, `.svg`, `.mp4`, `.pdf`, `.txt`, `.wasm`), with `application/octet-stream` as fallback
4. Generate HTML directory listings when the path resolves to a directory (sortable by name, size, modification time), with a configurable option to disable listings
5. Implement HTTP range requests: parse the `Range` header, respond with `206 Partial Content` and correct `Content-Range` header for single ranges. Support `bytes=start-end`, `bytes=start-`, and `bytes=-suffix`
6. Generate ETags based on file content hash (or size + modification time). Return `304 Not Modified` when `If-None-Match` matches the current ETag
7. Implement transparent gzip compression: when `Accept-Encoding` includes `gzip` and the file is compressible (text types, not images), compress the response body and set `Content-Encoding: gzip`
8. Path traversal prevention: reject any request where the resolved path escapes the document root after symlink resolution. Return 403 Forbidden
9. Return proper HTTP status codes: 200, 206, 304, 403, 404, 405 (method not allowed for non-GET/HEAD)
10. Support `HEAD` requests (same headers as GET, but no body)

## Hints

<details>
<summary>Hint 1: Parsing HTTP/1.1 requests from raw TCP</summary>

Read from the connection until you find `\r\n\r\n` which terminates the headers. The first line is `METHOD /path HTTP/1.1`. Each subsequent line before the blank line is a header: `Key: Value`. You don't need to parse the body for a static file server (GET and HEAD have no body).

```go
scanner := bufio.NewReader(conn)
requestLine, _ := scanner.ReadString('\n')
// parse method, path, version from requestLine
```
</details>

<details>
<summary>Hint 2: Range request parsing</summary>

The `Range` header format is `bytes=start-end`. Parse the three cases: `bytes=0-499` (first 500 bytes), `bytes=500-` (from byte 500 to end), `bytes=-500` (last 500 bytes). Open the file, seek to the start position, and read only the requested number of bytes. Set status 206 and header `Content-Range: bytes start-end/total`.
</details>

<details>
<summary>Hint 3: ETag generation strategy</summary>

Computing a hash of file content on every request is expensive. A faster approach: combine the file size and modification time into a string and hash that. The ETag changes whenever the file is modified, which is the correct behavior. Format: `"size-mtime"` (quoted, as per HTTP spec).

```go
etag := fmt.Sprintf(`"%x-%x"`, info.Size(), info.ModTime().UnixNano())
```
</details>

<details>
<summary>Hint 4: Path traversal prevention</summary>

After joining the document root with the URL path, call `filepath.Clean` and then verify the result starts with the document root. But this is not enough -- symlinks can escape the root. Use `filepath.EvalSymlinks` to resolve the real path, then check the prefix again.

```go
resolved, _ := filepath.EvalSymlinks(joined)
if !strings.HasPrefix(resolved, docRoot) {
    // path traversal attempt
}
```
</details>

## Acceptance Criteria

- [ ] Server accepts TCP connections and correctly parses HTTP/1.1 GET and HEAD requests
- [ ] Files are served with correct MIME types based on extension
- [ ] Directory listings display file names, sizes, and modification times as HTML
- [ ] Range requests return 206 with correct `Content-Range` for all three range formats
- [ ] ETags are generated and `304 Not Modified` is returned when `If-None-Match` matches
- [ ] Gzip compression is applied to text-based files when client supports it
- [ ] Requests containing `..` or symlinks that escape the document root return 403
- [ ] HEAD requests return headers without body
- [ ] Non-GET/HEAD methods return 405 Method Not Allowed
- [ ] Server handles concurrent requests without races or file descriptor leaks

## Research Resources

- [RFC 9110: HTTP Semantics -- Range Requests](https://httpwg.org/specs/rfc9110.html#range.requests) -- authoritative spec for range requests and conditional requests
- [RFC 9110: HTTP Semantics -- Conditional Requests](https://httpwg.org/specs/rfc9110.html#conditional.requests) -- ETag and If-None-Match semantics
- [MDN: Range requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests) -- practical guide with examples
- [MDN: Content negotiation](https://developer.mozilla.org/en-US/docs/Web/HTTP/Content_negotiation) -- Accept-Encoding and content type negotiation
- [OWASP: Path Traversal](https://owasp.org/www-community/attacks/Path_Traversal) -- attack vectors and prevention strategies
- [Go `mime` package](https://pkg.go.dev/mime) -- MIME type detection utilities
