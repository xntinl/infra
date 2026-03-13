# 15. Custom HTTP Transport

<!--
difficulty: advanced
concepts: [http-transport, round-tripper, dial-context, connection-control, proxy, transport-middleware]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [http-keep-alive-analysis, http-client-instrumentation, dns-resolver-and-custom-dialer]
-->

## Prerequisites

- Go 1.22+ installed
- Completed HTTP Keep-Alive Analysis and HTTP Client Instrumentation exercises
- Understanding of `http.RoundTripper` and `http.Transport`
- Familiarity with custom dialers and DNS resolution

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** custom `http.Transport` configurations for production workloads with tuned timeouts, pool sizes, and TLS settings
- **Implement** `http.RoundTripper` middleware that transforms requests and responses (header injection, retry, circuit breaking)
- **Configure** transport-level proxy support, custom TLS configs, and HTTP/2 settings
- **Analyze** the interaction between `http.Transport`, `net.Dialer`, and TLS configuration

## Why Custom HTTP Transports Matter

The default `http.Transport` works for simple use cases, but production systems need fine-grained control. You might need to inject headers into every request, retry failed requests with backoff, route through a corporate proxy, pin TLS certificates, or limit concurrency to a backend. All of this happens at the transport layer, below the `http.Client` and above the TCP connection.

## The Problem

Build a composable HTTP transport system with:

1. A base transport with production-tuned settings
2. Middleware transports that chain via `http.RoundTripper` wrapping
3. Specific middleware for header injection, retry with backoff, and request logging

## Requirements

1. **Production transport** -- configure `http.Transport` with appropriate `MaxIdleConns`, `MaxConnsPerHost`, `TLSHandshakeTimeout`, `ResponseHeaderTimeout`, custom `DialContext`, and `ForceAttemptHTTP2`
2. **Header injection middleware** -- implement a `RoundTripper` that adds configurable headers (User-Agent, X-Request-ID, Authorization) to every outgoing request without modifying the original request
3. **Retry middleware** -- implement a `RoundTripper` that retries on 5xx responses and network errors with exponential backoff, configurable max attempts, and a jitter factor
4. **Logging middleware** -- implement a `RoundTripper` that logs method, URL, status code, duration, and response size for every request
5. **Request cloning** -- when retrying, clone the request properly including the body using `GetBody`
6. **Composability** -- demonstrate chaining: logging -> retry -> header injection -> base transport
7. **Tests** -- test each middleware independently and the composed chain against an `httptest.Server`

## Hints

<details>
<summary>Hint 1: RoundTripper middleware pattern</summary>

```go
type HeaderTransport struct {
    Inner   http.RoundTripper
    Headers map[string]string
}

func (t *HeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // Clone the request to avoid mutating the caller's request
    clone := req.Clone(req.Context())
    for k, v := range t.Headers {
        clone.Header.Set(k, v)
    }
    return t.Inner.RoundTrip(clone)
}
```

</details>

<details>
<summary>Hint 2: Retry with body cloning</summary>

```go
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    var resp *http.Response
    var err error

    for attempt := 0; attempt <= t.MaxRetries; attempt++ {
        if attempt > 0 {
            // Must re-create the body for retries
            if req.GetBody != nil {
                req.Body, _ = req.GetBody()
            }
            delay := t.BaseDelay * time.Duration(1<<uint(attempt-1))
            time.Sleep(delay)
        }
        resp, err = t.Inner.RoundTrip(req)
        if err == nil && resp.StatusCode < 500 {
            return resp, nil
        }
        if resp != nil {
            io.Copy(io.Discard, resp.Body)
            resp.Body.Close()
        }
    }
    return resp, err
}
```

</details>

<details>
<summary>Hint 3: Composing the chain</summary>

```go
base := &http.Transport{
    MaxIdleConns:          100,
    MaxConnsPerHost:       10,
    IdleConnTimeout:       90 * time.Second,
    TLSHandshakeTimeout:  10 * time.Second,
    ResponseHeaderTimeout: 30 * time.Second,
    ForceAttemptHTTP2:     true,
}

transport := &LoggingTransport{
    Inner: &RetryTransport{
        Inner: &HeaderTransport{
            Inner:   base,
            Headers: map[string]string{"User-Agent": "myapp/1.0"},
        },
        MaxRetries: 3,
        BaseDelay:  100 * time.Millisecond,
    },
}

client := &http.Client{Transport: transport}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Header middleware adds headers without modifying the original request
- Retry middleware retries on 500 responses and succeeds when the server recovers
- Retry middleware gives up after max attempts and returns the last error
- Logging middleware records correct method, URL, status, and duration
- The composed chain applies middleware in the correct order

## What's Next

Continue to [16 - Reverse Proxy with Header Manipulation](../16-reverse-proxy-header-manipulation/16-reverse-proxy-header-manipulation.md) to build HTTP reverse proxies with request and response transformation.

## Summary

- `http.RoundTripper` is the single-method interface (`RoundTrip`) that `http.Client` calls for every request
- Middleware transports wrap an inner `RoundTripper` to add behavior (logging, retry, headers) without modifying application code
- Always clone requests before modifying headers to avoid mutating the caller's request
- For retry, use `req.GetBody()` to re-create the request body on each attempt
- Production transports need tuned timeouts, pool sizes, and TLS settings
- Compose middleware by nesting: the outermost wrapper runs first, the innermost calls the base transport

## Reference

- [http.RoundTripper](https://pkg.go.dev/net/http#RoundTripper)
- [http.Transport](https://pkg.go.dev/net/http#Transport)
- [http.Request.Clone](https://pkg.go.dev/net/http#Request.Clone)
