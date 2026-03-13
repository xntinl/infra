# 12. HTTP Client Instrumentation

<!--
difficulty: advanced
concepts: [httptrace, client-trace, dns-timing, tls-timing, round-tripper, request-lifecycle]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [http-keep-alive-analysis, dns-resolver-and-custom-dialer, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Completed HTTP Keep-Alive Analysis exercise
- Understanding of HTTP request lifecycle (DNS, connect, TLS, send, receive)
- Familiarity with `net/http/httptrace`

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** full HTTP request lifecycle tracing using `httptrace.ClientTrace`
- **Measure** each phase of a request: DNS lookup, TCP connect, TLS handshake, time-to-first-byte, and body transfer
- **Design** a reusable instrumented `http.RoundTripper` that captures timing metrics for all requests
- **Analyze** latency breakdowns to identify bottlenecks in HTTP client communication

## Why HTTP Client Instrumentation Matters

When an HTTP request is slow, you need to know which phase is slow: DNS resolution? TCP handshake? TLS negotiation? Server think time? Body transfer? Without instrumentation, all you see is total latency. Go's `httptrace` package exposes hooks at every stage of the request lifecycle, letting you build precise latency breakdowns for every outgoing request.

## The Problem

Build an instrumented HTTP client that captures per-phase timing for every request and exposes metrics through a reusable `http.RoundTripper` wrapper.

## Requirements

1. **Trace all phases** -- use `httptrace.ClientTrace` to capture: DNS start/done, connect start/done, TLS handshake start/done, got first response byte, got connection
2. **Timing struct** -- define a `RequestTiming` struct that records duration for each phase: DNS, connect, TLS, server processing (TTFB minus connect done), body transfer
3. **Round tripper wrapper** -- implement `http.RoundTripper` that wraps an inner transport, injects tracing, and collects `RequestTiming` for each request
4. **Metrics aggregation** -- track min, max, mean, and p99 durations per phase across multiple requests
5. **Retry-aware** -- handle the case where connections are reused (DNS and connect phases report zero duration)
6. **Logging** -- optionally log a per-request timing waterfall (DNS: 2ms | Connect: 5ms | TLS: 12ms | TTFB: 45ms | Body: 3ms)
7. **Tests** -- test against an `httptest.Server` and verify that timing values are non-negative and phases sum approximately to total

## Hints

<details>
<summary>Hint 1: ClientTrace hooks for timing</summary>

```go
var dnsStart, connectStart, tlsStart time.Time

trace := &httptrace.ClientTrace{
    DNSStart:             func(_ httptrace.DNSStartInfo) { dnsStart = time.Now() },
    DNSDone:              func(_ httptrace.DNSDoneInfo) { timing.DNS = time.Since(dnsStart) },
    ConnectStart:         func(_, _ string) { connectStart = time.Now() },
    ConnectDone:          func(_, _ string, _ error) { timing.Connect = time.Since(connectStart) },
    TLSHandshakeStart:    func() { tlsStart = time.Now() },
    TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { timing.TLS = time.Since(tlsStart) },
    GotFirstResponseByte: func() { timing.TTFB = time.Since(requestStart) },
}
```

</details>

<details>
<summary>Hint 2: RoundTripper wrapper</summary>

```go
type InstrumentedTransport struct {
    Inner    http.RoundTripper
    OnTiming func(RequestTiming)
}

func (t *InstrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    timing := &RequestTiming{URL: req.URL.String()}
    trace := buildTrace(timing)
    req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
    timing.Start = time.Now()
    resp, err := t.Inner.RoundTrip(req)
    timing.Total = time.Since(timing.Start)
    if t.OnTiming != nil {
        t.OnTiming(*timing)
    }
    return resp, err
}
```

</details>

<details>
<summary>Hint 3: P99 calculation</summary>

```go
func p99(durations []time.Duration) time.Duration {
    if len(durations) == 0 {
        return 0
    }
    sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
    idx := int(float64(len(durations)) * 0.99)
    if idx >= len(durations) {
        idx = len(durations) - 1
    }
    return durations[idx]
}
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- All timing phases are captured with non-negative durations
- DNS and connect phases are zero for reused connections
- TLS phase is non-zero only for HTTPS connections
- The sum of phases approximately equals total request duration
- The RoundTripper wrapper does not alter request/response behavior

## What's Next

Continue to [13 - gRPC Streaming](../13-grpc-streaming/13-grpc-streaming.md) to build streaming RPC services using gRPC.

## Summary

- `httptrace.ClientTrace` provides hooks at every phase of the HTTP request lifecycle
- Timing breakdowns reveal whether latency comes from DNS, connect, TLS, server processing, or body transfer
- Wrapping `http.RoundTripper` provides instrumentation without modifying calling code
- Reused connections skip DNS and connect phases, so instrumentation must handle zero durations
- Aggregate metrics (min, max, mean, p99) across requests to detect performance regressions
- Response body transfer time is measured between `GotFirstResponseByte` and body read completion

## Reference

- [net/http/httptrace](https://pkg.go.dev/net/http/httptrace)
- [Go Blog: Introducing HTTP Tracing](https://go.dev/blog/http-tracing)
- [http.RoundTripper](https://pkg.go.dev/net/http#RoundTripper)
