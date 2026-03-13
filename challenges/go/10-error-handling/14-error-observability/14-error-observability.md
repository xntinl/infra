# 14. Error Observability

<!--
difficulty: insane
concepts: [error-logging, error-metrics, distributed-tracing, structured-logging, error-rates, slog]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [custom-error-types, structured-error-types, http-middleware, context]
-->

## The Challenge

Build an error observability layer for a Go HTTP service. Errors should not just be returned -- they should be logged with structured context, counted as metrics, and correlated with distributed traces. When an oncall engineer gets paged, they should be able to find the error in logs, see the request that caused it, check the error rate on a dashboard, and trace it through multiple services.

## Requirements

### Core Components

1. **Structured Error Logging with `log/slog`**
   - Every error response must be logged with structured fields: `error_code`, `http_status`, `request_id`, `method`, `path`, `duration_ms`
   - Errors must be logged at the appropriate level: 4xx at `Warn`, 5xx at `Error`
   - Internal error details (stack traces, wrapped causes) must appear in logs but never in HTTP responses

2. **Error Metrics**
   - Track error counts by: `code` (error code), `method` (HTTP method), `path` (route pattern)
   - Implement a simple in-memory metrics collector (no external dependencies required)
   - Expose metrics at `GET /metrics` in a simple text format:
     ```
     http_errors_total{code="NOT_FOUND",method="GET",path="/users/{id}"} 3
     http_errors_total{code="VALIDATION_FAILED",method="POST",path="/users"} 7
     ```

3. **Request Context Propagation**
   - Generate a request ID for each incoming request (use `X-Request-ID` header if present, otherwise generate a UUID)
   - Store the request ID in the context
   - Include the request ID in all log entries and error responses
   - Pass the request ID downstream in outgoing requests

4. **Error Middleware Integration**
   - Build on the error middleware pattern from exercise 10
   - The middleware should: log the error, increment metrics, add trace context, then write the HTTP response
   - Separate the concerns: error creation (in handlers), error observation (in middleware), error response (in middleware)

### Application Endpoints

Implement a small service with these endpoints to exercise the observability layer:

- `POST /users` -- validates input, may return validation errors
- `GET /users/{id}` -- may return not-found or internal errors
- `GET /metrics` -- returns error metrics
- `GET /health` -- always succeeds

### Deliverables

A single-binary Go program that:
- Starts an HTTP server on `:8080`
- Logs errors as structured JSON using `log/slog`
- Tracks error metrics in memory
- Propagates request IDs through context
- Demonstrates all error observability features

## Hints

<details>
<summary>Hint 1: slog structured logging</summary>

```go
slog.Error("request failed",
    "error_code", appErr.Code,
    "http_status", appErr.HTTPStatus,
    "request_id", requestID,
    "method", r.Method,
    "path", r.URL.Path,
    "duration_ms", time.Since(start).Milliseconds(),
    "error", appErr.Error(),
)
```
</details>

<details>
<summary>Hint 2: Simple metrics collector</summary>

```go
type MetricsCollector struct {
    mu      sync.Mutex
    counters map[string]int64
}

func (m *MetricsCollector) Inc(code, method, path string) {
    key := fmt.Sprintf("http_errors_total{code=%q,method=%q,path=%q}", code, method, path)
    m.mu.Lock()
    m.counters[key]++
    m.mu.Unlock()
}
```
</details>

<details>
<summary>Hint 3: Request ID middleware</summary>

```go
type ctxKey string
const requestIDKey ctxKey = "request_id"

func requestIDMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := r.Header.Get("X-Request-ID")
        if id == "" {
            id = generateID()
        }
        ctx := context.WithValue(r.Context(), requestIDKey, id)
        w.Header().Set("X-Request-ID", id)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```
</details>

<details>
<summary>Hint 4: Composing middleware</summary>

Stack middleware in order: request ID -> error observation -> route handler.

```go
mux := http.NewServeMux()
// register routes...
handler := requestIDMiddleware(mux)
http.ListenAndServe(":8080", handler)
```

The error observation happens inside `wrapHandler`, which has access to the request context (and thus the request ID).
</details>

## Success Criteria

- [ ] All errors are logged as structured JSON with `slog`
- [ ] Log level matches severity: `Warn` for 4xx, `Error` for 5xx
- [ ] Logs include `error_code`, `http_status`, `request_id`, `method`, `path`, `duration_ms`
- [ ] Error metrics are tracked by code, method, and path
- [ ] `GET /metrics` returns current error counts
- [ ] Request IDs are generated or extracted from `X-Request-ID`
- [ ] Request IDs appear in log output and `X-Request-ID` response header
- [ ] Internal error details (wrapped causes) appear in logs but NOT in HTTP response bodies
- [ ] The program compiles and runs with only standard library dependencies (`log/slog`, `net/http`, `sync`, etc.)

## Research Resources

- [log/slog package](https://pkg.go.dev/log/slog) -- Go's structured logging (Go 1.21+)
- [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/) -- distributed tracing and metrics
- [Prometheus client_golang](https://github.com/prometheus/client_golang) -- production metrics library (for reference, not required)
- [Google SRE Book: Monitoring Distributed Systems](https://sre.google/sre-book/monitoring-distributed-systems/) -- philosophy behind error observability
- [Peter Bourgon: Logging v. instrumentation](https://peter.bourgon.org/blog/2016/02/07/logging-v-instrumentation.html) -- when to log vs. when to use metrics
