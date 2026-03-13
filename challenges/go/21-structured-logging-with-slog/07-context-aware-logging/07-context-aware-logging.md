# 7. Context-Aware Logging

<!--
difficulty: advanced
concepts: [context-values, trace-id, correlation-id, slog-handler-context, middleware-logging]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [slog-basics, custom-slog-handler, context-package, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Custom Slog Handler](../06-custom-slog-handler/06-custom-slog-handler.md)
- Familiarity with `context.Context` and HTTP middleware

## Learning Objectives

After completing this exercise, you will be able to:

- **Extract** values from `context.Context` and inject them into log records
- **Build** a handler wrapper that enriches logs with trace and request IDs
- **Design** HTTP middleware that creates context-aware loggers

## Why Context-Aware Logging

In a web service handling hundreds of concurrent requests, you need to correlate log lines that belong to the same request. Passing a logger through every function works but is intrusive. Storing the logger or its attributes in `context.Context` lets you extract trace IDs, request IDs, and user IDs in a custom handler without changing function signatures across your codebase.

## The Problem

Build a handler wrapper that reads a trace ID and request ID from `context.Context` and automatically injects them into every log record. Then build HTTP middleware that sets these values.

### Requirements

1. Define context keys for trace ID and request ID
2. Build a `ContextHandler` that wraps any `slog.Handler` and extracts context values
3. Write helper functions to store and retrieve IDs from context
4. Build an HTTP middleware that generates a request ID, stores it in context, and logs request start/end
5. Demonstrate that downstream log calls automatically include the request ID

### Hints

<details>
<summary>Hint 1: Context key pattern</summary>

```go
type ctxKey string

const (
    traceIDKey   ctxKey = "trace_id"
    requestIDKey ctxKey = "request_id"
)

func WithTraceID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, traceIDKey, id)
}

func TraceIDFromContext(ctx context.Context) string {
    if id, ok := ctx.Value(traceIDKey).(string); ok {
        return id
    }
    return ""
}
```
</details>

<details>
<summary>Hint 2: ContextHandler wrapping</summary>

```go
type ContextHandler struct {
    inner slog.Handler
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
    if traceID := TraceIDFromContext(ctx); traceID != "" {
        r.AddAttrs(slog.String("trace_id", traceID))
    }
    if reqID := RequestIDFromContext(ctx); reqID != "" {
        r.AddAttrs(slog.String("request_id", reqID))
    }
    return h.inner.Handle(ctx, r)
}
```

Remember to delegate `Enabled`, `WithAttrs`, and `WithGroup` to the inner handler.
</details>

<details>
<summary>Hint 3: Using slog.InfoContext</summary>

Use the `Context` variants of logging functions to pass context:

```go
slog.InfoContext(ctx, "processing request", "path", r.URL.Path)
```

The handler receives this context in its `Handle` method.
</details>

## Verification

Your program should produce output where every log line from a request handler includes `trace_id` and `request_id`, even though those values were never passed to the slog call explicitly:

```
{"time":"...","level":"INFO","msg":"request started","trace_id":"abc-123","request_id":"req-001","method":"GET","path":"/api/users"}
{"time":"...","level":"INFO","msg":"fetching from database","trace_id":"abc-123","request_id":"req-001","table":"users"}
{"time":"...","level":"INFO","msg":"request completed","trace_id":"abc-123","request_id":"req-001","status":200,"latency":"12ms"}
```

```bash
go run main.go &
curl http://localhost:8080/api/users
kill %1
```

## What's Next

Continue to [08 - Log Sampling for High Throughput](../08-log-sampling/08-log-sampling.md) to build a handler that samples logs to reduce volume without losing visibility.

## Summary

- `slog.InfoContext(ctx, ...)` and similar functions pass context to the handler
- A `ContextHandler` wrapper extracts values from context and adds them as attributes
- Context keys should be unexported custom types to avoid collisions
- HTTP middleware generates request/trace IDs and stores them in context
- Downstream code uses context-aware log functions without knowing about the IDs
- This pattern decouples log enrichment from business logic

## Reference

- [slog.InfoContext](https://pkg.go.dev/log/slog#InfoContext)
- [slog.Handler interface](https://pkg.go.dev/log/slog#Handler)
- [context.WithValue](https://pkg.go.dev/context#WithValue)
