<!-- difficulty: advanced -->
<!-- category: security-networking -->
<!-- languages: [go] -->
<!-- concepts: [tls-termination, sni-routing, reverse-proxy, certificate-management, health-checking, connection-draining, http-headers] -->
<!-- estimated_time: 12-18 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [go-basics, tcp-networking, http-protocol, tls-fundamentals, concurrency-goroutines, http-headers, load-balancing-concepts] -->

# Challenge 111: Reverse Proxy with TLS Termination

## Languages

Go 1.22+

## Prerequisites

- TCP server programming in Go (`net.Listener`, `net.Conn`)
- Understanding of TLS at the protocol level: certificates, private keys, SNI (Server Name Indication)
- HTTP request/response structure: headers, methods, status codes, chunked transfer
- Go's `crypto/tls` package: `tls.Config`, `tls.Certificate`, SNI callback
- Goroutine-based concurrency for handling multiple connections
- Familiarity with reverse proxy concepts: forwarding requests, modifying headers, backend health

## Learning Objectives

- **Implement** a reverse proxy that terminates TLS connections and forwards plaintext HTTP to backend servers
- **Design** SNI-based routing that maps different domain names to different backend pools using the TLS ClientHello
- **Analyze** the security implications of TLS termination: where encryption ends, what headers to set, what information is exposed
- **Evaluate** the operational tradeoffs of health checking strategies and graceful connection draining during backend changes
- **Implement** HTTP header manipulation for correct proxying: X-Forwarded-For, X-Forwarded-Proto, Via, and hop-by-hop header removal

## The Challenge

Every HTTPS website sits behind a TLS termination point -- a server that decrypts the incoming TLS connection, inspects the plaintext HTTP request, and forwards it to a backend application server over plain HTTP (or re-encrypts it for backend TLS). NGINX, HAProxy, Envoy, and Caddy all do this. The termination proxy is the most security-critical component in the infrastructure: it holds the private keys, it sees all plaintext traffic, and it makes routing decisions.

Build a reverse proxy in Go that terminates TLS and routes to backend servers. The proxy listens on port 443 (or a configurable port) for HTTPS connections. When a TLS ClientHello arrives, the proxy reads the SNI (Server Name Indication) field to determine which domain the client is requesting. Based on the SNI, it selects the appropriate TLS certificate and backend server pool. After the TLS handshake completes, the proxy reads the plaintext HTTP request, adds forwarding headers, and sends it to a backend over plain HTTP. The backend response is relayed back through the TLS connection.

Use Go's `crypto/tls` package for TLS handling, but build the proxy logic, routing, health checking, and connection management from scratch. Do not use `net/http/httputil.ReverseProxy` -- implement the request forwarding yourself to understand what a reverse proxy actually does at the byte level.

## Requirements

1. Accept TLS connections with configurable certificate/key pairs loaded from PEM files. Support loading multiple certificates for multiple domains
2. Implement SNI-based routing: read the server name from the TLS ClientHello via `tls.Config.GetCertificate` and select the appropriate certificate and backend pool. Return an error for unknown SNI values
3. Implement a routing table: map domain names to backend pools. Each pool contains one or more backend addresses (`host:port`). Configuration is loaded from a JSON or YAML file
4. Parse the plaintext HTTP request from the decrypted TLS connection: method, path, headers, and body. Do not use `net/http` for parsing incoming requests -- parse the raw bytes from the TLS connection to understand the protocol. You may use `net/http` for the outgoing connection to backends
5. Add proxy headers before forwarding:
   - `X-Forwarded-For`: client IP address (append to existing header if present)
   - `X-Forwarded-Proto`: `https` (the original protocol before termination)
   - `X-Forwarded-Host`: original Host header value
   - `Via`: proxy identifier
6. Remove hop-by-hop headers before forwarding: `Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailers`, `Transfer-Encoding`, `Upgrade` (except for WebSocket upgrades)
7. Forward the request to a backend from the selected pool. If the pool has multiple backends, use round-robin selection
8. Relay the backend response back to the client through the TLS connection, preserving status code, headers, and body
9. Implement backend health checking: periodically (configurable interval) send HTTP GET requests to a health endpoint (default `/health`) on each backend. Mark unhealthy backends as down and skip them in routing. Re-check and restore when they recover
10. Implement graceful connection draining: when receiving SIGTERM, stop accepting new connections, wait for in-flight requests to complete (up to a configurable timeout), then shut down
11. Implement access logging: log each request with timestamp, client IP, SNI domain, method, path, backend target, response status, and latency
12. Handle connection timeouts: configurable read timeout, write timeout, and idle timeout for client connections. Configurable timeout for backend connections
13. Support HTTP/1.1 keep-alive: reuse the TLS connection for multiple requests from the same client
14. Generate self-signed certificates for testing using Go's `crypto/x509` and `crypto/rsa` packages

## Hints

1. The SNI callback `GetCertificate` receives a `*tls.ClientHelloInfo` with the `ServerName`
   field. Use this to look up the correct certificate. If no certificate matches, return an
   error or a default certificate:

   ```go
   tlsConfig := &tls.Config{
       GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
           cert, ok := certMap[hello.ServerName]
           if !ok {
               return nil, fmt.Errorf("no certificate for %s", hello.ServerName)
           }
           return cert, nil
       },
   }
   ```

2. Raw HTTP request parsing from a `tls.Conn`: read bytes until you find `\r\n\r\n` (end of
   headers). The first line is `METHOD PATH HTTP/1.1\r\n`. Each subsequent line is
   `Header-Name: value\r\n`. For requests with a body, read `Content-Length` bytes after the
   header section. For chunked encoding, read chunk-size lines and data until the `0\r\n`
   terminator.

3. For graceful shutdown, use a `sync.WaitGroup` to track in-flight requests. When SIGTERM
   arrives, close the listener (stop accepting), then `wg.Wait()` with a timeout. Use
   `context.WithTimeout` to enforce the drain deadline.

4. Health checking runs in a background goroutine. Use a `sync.RWMutex` to protect the
   healthy/unhealthy state of each backend. The health checker writes (exclusive lock), and
   the router reads (shared lock). This avoids blocking request routing during health checks.

## Acceptance Criteria

- [ ] Proxy accepts TLS connections and completes the handshake with the correct certificate for the requested SNI domain
- [ ] Unknown SNI values are rejected (connection closed or default error certificate)
- [ ] Multiple domains with different certificates route to different backend pools
- [ ] HTTP requests are parsed from the raw TLS connection bytes (method, path, headers, body)
- [ ] `X-Forwarded-For` header contains the client IP address
- [ ] `X-Forwarded-Proto` is set to `https`
- [ ] `X-Forwarded-Host` contains the original Host header
- [ ] Hop-by-hop headers are removed before forwarding
- [ ] Round-robin distributes requests across backends in order
- [ ] Backend responses (status code, headers, body) are relayed to the client unchanged
- [ ] Health checks detect unhealthy backends and exclude them from routing
- [ ] Recovered backends are restored to the routing pool after passing a health check
- [ ] SIGTERM triggers graceful shutdown: no new connections accepted, in-flight requests complete
- [ ] Access logs contain timestamp, client IP, SNI, method, path, backend, status, and latency
- [ ] Client connection timeouts are enforced (read, write, idle)
- [ ] Keep-alive: multiple sequential requests over one TLS connection work correctly
- [ ] Self-signed certificates are generated for testing and the proxy loads them correctly
- [ ] No use of `httputil.ReverseProxy` -- all proxy logic is hand-written
- [ ] All tests pass with `go test ./...`

## Research Resources

- [Go `crypto/tls` package documentation](https://pkg.go.dev/crypto/tls) -- TLS configuration, certificate loading, SNI callback (`GetCertificate`)
- [RFC 6066: TLS Extensions (SNI)](https://datatracker.ietf.org/doc/html/rfc6066#section-3) -- the Server Name Indication extension specification
- [RFC 7239: Forwarded HTTP Extension](https://datatracker.ietf.org/doc/html/rfc7239) -- standardized forwarding header (alternative to X-Forwarded-For)
- [NGINX Reverse Proxy Documentation](https://docs.nginx.com/nginx/admin-guide/web-server/reverse-proxy/) -- reference architecture for reverse proxy features
- [Envoy Proxy Architecture Overview](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/arch_overview) -- modern proxy design with health checking, connection draining, and observability
- [HTTP/1.1 Specification (RFC 9110)](https://datatracker.ietf.org/doc/html/rfc9110) -- definitive HTTP semantics reference, especially hop-by-hop headers (Section 7.6.1)
- [Go TLS Certificate Generation](https://pkg.go.dev/crypto/x509) -- programmatic certificate creation for testing
- [Graceful Shutdown in Go](https://pkg.go.dev/net/http#Server.Shutdown) -- patterns for connection draining (for reference; implement your own for raw TCP)
