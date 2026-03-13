<!--
difficulty: advanced
concepts: request-id, correlation-id, middleware, context-values, distributed-tracing
tools: net/http, context, github.com/google/uuid, log/slog
estimated_time: 30m
bloom_level: applying
prerequisites: http-middleware, context, logging-basics
-->

# Exercise 30.5: Request ID Propagation

## Prerequisites

Before starting this exercise, you should be comfortable with:

- HTTP middleware patterns
- `context.Context` and context values
- Structured logging
- HTTP client and server basics

## Learning Objectives

By the end of this exercise, you will be able to:

1. Generate and attach a unique request ID to every incoming HTTP request
2. Propagate request IDs through context into all downstream operations
3. Include request IDs in structured log output automatically
4. Forward request IDs to downstream HTTP services via headers

## Why This Matters

When a user reports "my request failed," you need to find the exact request across potentially dozens of services and millions of log lines. A request ID (also called correlation ID) is a unique string that follows a request through every service, log entry, and database query. Without it, debugging distributed systems is like finding a needle in a haystack.

---

## Problem

Build an HTTP middleware that assigns a request ID to every request and propagates it through the entire request lifecycle. Implement a two-service setup where Service A calls Service B, and the request ID flows end-to-end.

### Hints

- Use the `X-Request-ID` header as the standard header name
- If the incoming request already has a request ID (from a load balancer or upstream service), preserve it
- Store the request ID in the request context using a custom context key type (not a bare string)
- Use `log/slog` with a handler that automatically extracts the request ID from context
- In the HTTP client, extract the request ID from context and set it on the outgoing request header

### Step 1: Create the project

```bash
mkdir -p request-id && cd request-id
go mod init request-id
go get github.com/google/uuid
```

### Step 2: Build the request ID package

Create `requestid.go`:

```go
package main

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-ID"

type contextKey struct{}

// FromContext extracts the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(contextKey{}).(string); ok {
		return id
	}
	return ""
}

// WithRequestID returns a new context with the given request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// Middleware assigns or preserves a request ID on every request.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = uuid.New().String()
		}

		ctx := WithRequestID(r.Context(), id)
		r = r.WithContext(ctx)

		// Set the request ID on the response headers too
		w.Header().Set(HeaderRequestID, id)

		next.ServeHTTP(w, r)
	})
}

// Transport is an http.RoundTripper that propagates request IDs to outgoing requests.
type RequestIDTransport struct {
	Base http.RoundTripper
}

func (t *RequestIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id := RequestIDFromContext(req.Context())
	if id != "" {
		req = req.Clone(req.Context())
		req.Header.Set(HeaderRequestID, id)
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
```

### Step 3: Build the logging integration

Create `logging.go`:

```go
package main

import (
	"context"
	"log/slog"
	"os"
)

// NewLogger creates a structured logger that includes request IDs.
func NewLogger() *slog.Logger {
	handler := &requestIDHandler{
		inner: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	}
	return slog.New(handler)
}

type requestIDHandler struct {
	inner slog.Handler
}

func (h *requestIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.inner.Handle(ctx, r)
}

func (h *requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &requestIDHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *requestIDHandler) WithGroup(name string) slog.Handler {
	return &requestIDHandler{inner: h.inner.WithGroup(name)}
}
```

### Step 4: Build the two-service demo

Create `main.go`:

```go
package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := NewLogger()

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &RequestIDTransport{},
	}

	// Service B (downstream)
	muxB := http.NewServeMux()
	muxB.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger.InfoContext(ctx, "Service B: processing request",
			"path", r.URL.Path)
		time.Sleep(50 * time.Millisecond) // simulate work
		fmt.Fprintf(w, `{"result": "data from service B"}`)
	})

	serverB := &http.Server{Addr: ":8081", Handler: RequestIDMiddleware(muxB)}
	go func() {
		logger.Info("Service B listening on :8081")
		serverB.ListenAndServe()
	}()
	time.Sleep(100 * time.Millisecond)

	// Service A (upstream, calls B)
	muxA := http.NewServeMux()
	muxA.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := RequestIDFromContext(ctx)
		logger.InfoContext(ctx, "Service A: received request",
			"path", r.URL.Path)

		// Call Service B, propagating the request ID via context
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:8081/api/data", nil)
		resp, err := client.Do(req)
		if err != nil {
			logger.ErrorContext(ctx, "Service A: call to B failed", "error", err)
			http.Error(w, "downstream error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		logger.InfoContext(ctx, "Service A: got response from B",
			"status", resp.StatusCode)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"request_id": %q, "downstream": %s}`, id, body)
	})

	serverA := &http.Server{Addr: ":8080", Handler: RequestIDMiddleware(muxA)}

	logger.Info("Service A listening on :8080")
	if err := serverA.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
```

### Step 5: Test

```bash
go run . &
sleep 1

# Request without an ID (one will be generated)
curl -s localhost:8080/ | jq .

# Request with a pre-set ID
curl -s -H "X-Request-ID: my-custom-id-123" localhost:8080/ | jq .

kill %1
```

Check the server logs -- every log line from both Service A and Service B should contain the same `request_id` field.

---

## Verify

```bash
go build -o server . && ./server &
sleep 1
RESPONSE=$(curl -s -H "X-Request-ID: test-verify-id" localhost:8080/)
echo "$RESPONSE" | jq -r .request_id
kill %1
```

The response should contain `"request_id": "test-verify-id"`, confirming the ID was preserved from the incoming header through both services.

---

## What's Next

In the next exercise, you will build structured error responses that give API consumers actionable information when things go wrong.

## Summary

- Assign a unique request ID to every request; preserve existing IDs from upstream
- Store the ID in `context.Context` using a private key type to avoid collisions
- Custom `slog.Handler` wrappers automatically enrich log records with the request ID
- A custom `http.RoundTripper` propagates the ID to outgoing HTTP requests
- The `X-Request-ID` header is the de facto standard for request correlation

## Reference

- [context.WithValue](https://pkg.go.dev/context#WithValue)
- [log/slog package](https://pkg.go.dev/log/slog)
- [http.RoundTripper](https://pkg.go.dev/net/http#RoundTripper)
- [google/uuid](https://pkg.go.dev/github.com/google/uuid)
