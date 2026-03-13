# 16. Reverse Proxy with Header Manipulation

<!--
difficulty: advanced
concepts: [reverse-proxy, httputil, request-rewriting, response-modification, hop-by-hop-headers, x-forwarded]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [custom-http-transport, http-programming, concurrent-tcp-server]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Custom HTTP Transport exercise
- Understanding of HTTP headers, request/response lifecycle
- Familiarity with `net/http/httputil.ReverseProxy`

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** an HTTP reverse proxy using `httputil.ReverseProxy` with custom director and response modifier functions
- **Design** header manipulation rules for request rewriting (X-Forwarded-For, X-Request-ID, Host) and response transformation
- **Analyze** hop-by-hop header handling and why certain headers must not be forwarded
- **Apply** path-based routing to direct requests to different upstream backends

## Why Reverse Proxies Matter

Reverse proxies sit between clients and backend services, providing routing, load balancing, TLS termination, header manipulation, and caching. API gateways, CDNs, and service meshes all use reverse proxies. Go's `httputil.ReverseProxy` provides a solid foundation, but production use requires understanding how to manipulate requests before forwarding and responses before returning them to the client.

## The Problem

Build a reverse proxy that routes requests to different backends based on URL path, adds standard proxy headers, strips sensitive response headers, and injects request tracing.

## Requirements

1. **Path-based routing** -- route `/api/` to one backend, `/static/` to another, using a configurable routing table
2. **Request rewriting** -- use `Director` to set the upstream `Host` header, add `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`, and generate a `X-Request-ID` (UUID) if not present
3. **Response modification** -- use `ModifyResponse` to strip `Server` and `X-Powered-By` headers, add `X-Proxy-By` header, and log response status
4. **Hop-by-hop headers** -- correctly handle `Connection`, `Keep-Alive`, `Transfer-Encoding`, `Upgrade`, and other hop-by-hop headers that must not be forwarded
5. **Error handling** -- use `ErrorHandler` to return a custom error page when the upstream is unreachable
6. **Websocket upgrade** -- ensure the proxy passes through `Connection: Upgrade` for websocket requests
7. **Tests** -- test path routing, header injection, header stripping, error handling, and upstream failover

## Hints

<details>
<summary>Hint 1: ReverseProxy with Director</summary>

```go
proxy := &httputil.ReverseProxy{
    Director: func(req *http.Request) {
        target := routeToBackend(req.URL.Path)
        req.URL.Scheme = target.Scheme
        req.URL.Host = target.Host
        req.Host = target.Host

        // Add forwarding headers
        if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
            req.Header.Set("X-Forwarded-For", clientIP)
        }
        req.Header.Set("X-Forwarded-Proto", "http")

        if req.Header.Get("X-Request-ID") == "" {
            req.Header.Set("X-Request-ID", uuid.New().String())
        }
    },
}
```

</details>

<details>
<summary>Hint 2: ModifyResponse</summary>

```go
proxy.ModifyResponse = func(resp *http.Response) error {
    resp.Header.Del("Server")
    resp.Header.Del("X-Powered-By")
    resp.Header.Set("X-Proxy-By", "my-proxy/1.0")
    return nil
}
```

</details>

<details>
<summary>Hint 3: Custom error handler</summary>

```go
proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
    log.Printf("proxy error: %s %s -> %v", r.Method, r.URL.Path, err)
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadGateway)
    json.NewEncoder(w).Encode(map[string]string{
        "error":      "upstream unavailable",
        "request_id": r.Header.Get("X-Request-ID"),
    })
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Requests to `/api/` are forwarded to the API backend
- Requests to `/static/` are forwarded to the static backend
- `X-Forwarded-For` and `X-Request-ID` headers are added to upstream requests
- `Server` and `X-Powered-By` headers are stripped from responses
- Unreachable backends return a 502 with a JSON error body
- Existing `X-Request-ID` headers are preserved, not overwritten

## What's Next

Continue to [17 - WebSocket Binary Frames](../17-websocket-binary-frames/17-websocket-binary-frames.md) to handle binary WebSocket communication.

## Summary

- `httputil.ReverseProxy` handles the core proxying: copying request to upstream and response to client
- `Director` modifies the outbound request (URL, Host, headers) before it is sent upstream
- `ModifyResponse` transforms the response before it is returned to the client
- `ErrorHandler` provides custom error responses when the upstream fails
- Hop-by-hop headers (`Connection`, `Keep-Alive`) are per-hop and must not be forwarded
- `X-Forwarded-For`, `X-Forwarded-Proto`, and `X-Forwarded-Host` communicate client origin info to the upstream

## Reference

- [httputil.ReverseProxy](https://pkg.go.dev/net/http/httputil#ReverseProxy)
- [MDN: X-Forwarded-For](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Forwarded-For)
- [RFC 7230 Section 6.1 -- Connection Header](https://datatracker.ietf.org/doc/html/rfc7230#section-6.1)
