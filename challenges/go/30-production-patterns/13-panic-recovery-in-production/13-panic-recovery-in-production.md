<!--
difficulty: advanced
concepts: panic-recovery, middleware, stack-trace, error-reporting, graceful-degradation
tools: net/http, runtime/debug, log/slog, recover
estimated_time: 30m
bloom_level: applying
prerequisites: error-handling, http-middleware, goroutines, defer
-->

# Exercise 30.13: Panic Recovery in Production

## Prerequisites

Before starting this exercise, you should be comfortable with:

- `defer`, `panic`, and `recover` mechanics
- HTTP middleware patterns
- Structured logging
- Goroutine lifecycle

## Learning Objectives

By the end of this exercise, you will be able to:

1. Build HTTP middleware that recovers from panics without crashing the server
2. Capture and log stack traces with structured context (request ID, path, method)
3. Return a clean 500 response to the client instead of dropping the connection
4. Apply recovery to background goroutines that are not covered by HTTP middleware

## Why This Matters

A panic in a single HTTP handler should not crash your entire server. In production, panics happen -- nil pointer dereferences, index out of range, failed type assertions. Recovery middleware catches them, logs the stack trace for debugging, and returns a proper error response. Without it, one bad request takes down the process and all concurrent connections.

---

## Problem

Build a panic recovery system for production HTTP services. The system must:

1. Catch panics in HTTP handlers and return a 500 JSON response
2. Log the full stack trace with request context (method, path, request ID)
3. Distinguish between panics caused by `http.ErrAbortHandler` (which should re-panic) and other panics
4. Provide a recovery wrapper for background goroutines that logs and optionally restarts
5. Track panic counts as a metric

### Hints

- `recover()` must be called directly inside a deferred function -- it does not work through multiple call levels
- `runtime/debug.Stack()` captures the current goroutine's stack trace
- `http.ErrAbortHandler` is a sentinel panic value used to abort HTTP handlers; do not recover from it
- Wrap the `http.ResponseWriter` to detect whether headers have already been sent before writing the 500 response
- For goroutines, create a `SafeGo` helper that wraps the function in a deferred recovery

### Step 1: Create the project

```bash
mkdir -p panic-recovery && cd panic-recovery
go mod init panic-recovery
```

### Step 2: Build the recovery middleware

Create `recovery.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
)

var panicCount atomic.Int64

type statusWriter struct {
	http.ResponseWriter
	written bool
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.written = true
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	sw.written = true
	return sw.ResponseWriter.Write(b)
}

func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w}

			defer func() {
				if err := recover(); err != nil {
					// Re-panic for ErrAbortHandler (used to abort connections intentionally)
					if err == http.ErrAbortHandler {
						panic(err)
					}

					panicCount.Add(1)
					stack := debug.Stack()

					// Extract request context for logging
					requestID := r.Header.Get("X-Request-ID")

					logger.Error("panic recovered",
						"error", fmt.Sprintf("%v", err),
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", requestID,
						"remote_addr", r.RemoteAddr,
						"stack", string(stack),
						"panic_count", panicCount.Load(),
					)

					// Only write error response if headers haven't been sent
					if !sw.written {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusInternalServerError)
						json.NewEncoder(w).Encode(map[string]interface{}{
							"error":      "internal server error",
							"request_id": requestID,
						})
					}
				}
			}()

			next.ServeHTTP(sw, r)
		})
	}
}

// PanicCount returns the total number of recovered panics.
func PanicCount() int64 {
	return panicCount.Load()
}
```

### Step 3: Build the goroutine recovery helper

Create `safego.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

type SafeGoOption func(*safeGoConfig)

type safeGoConfig struct {
	logger     *slog.Logger
	name       string
	restart    bool
	maxRetries int
	backoff    time.Duration
}

func WithName(name string) SafeGoOption {
	return func(c *safeGoConfig) { c.name = name }
}

func WithRestart(maxRetries int, backoff time.Duration) SafeGoOption {
	return func(c *safeGoConfig) {
		c.restart = true
		c.maxRetries = maxRetries
		c.backoff = backoff
	}
}

// SafeGo runs fn in a goroutine with panic recovery.
func SafeGo(ctx context.Context, logger *slog.Logger, fn func(ctx context.Context), opts ...SafeGoOption) {
	cfg := &safeGoConfig{
		logger: logger,
		name:   "anonymous",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	go func() {
		retries := 0
		for {
			func() {
				defer func() {
					if err := recover(); err != nil {
						panicCount.Add(1)
						stack := debug.Stack()
						logger.Error("goroutine panic recovered",
							"goroutine", cfg.name,
							"error", fmt.Sprintf("%v", err),
							"stack", string(stack),
							"retry", retries,
						)
					}
				}()
				fn(ctx)
			}()

			// Check if we should restart
			if !cfg.restart || ctx.Err() != nil {
				return
			}

			retries++
			if cfg.maxRetries > 0 && retries > cfg.maxRetries {
				logger.Error("goroutine exceeded max retries",
					"goroutine", cfg.name,
					"retries", retries)
				return
			}

			logger.Info("restarting goroutine",
				"goroutine", cfg.name,
				"retry", retries,
				"backoff", cfg.backoff)
			time.Sleep(cfg.backoff)
		}
	}()
}
```

### Step 4: Build the demo

Create `main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	// Start a background worker with auto-restart on panic
	SafeGo(ctx, logger, func(ctx context.Context) {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count++
				logger.Info("background worker tick", "count", count)
				if count == 2 {
					// Simulate an unexpected panic
					panic("background worker: unexpected nil map access")
				}
			}
		}
	}, WithName("background-worker"), WithRestart(3, 2*time.Second))

	mux := http.NewServeMux()

	// Normal handler
	mux.HandleFunc("GET /api/safe", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Handler that panics
	mux.HandleFunc("GET /api/panic", func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = m["key"] // nil map access -> panic
	})

	// Handler with index out of range
	mux.HandleFunc("GET /api/index", func(w http.ResponseWriter, r *http.Request) {
		s := []int{1, 2, 3}
		fmt.Fprintln(w, s[10]) // index out of range -> panic
	})

	// Panic metrics
	mux.HandleFunc("GET /metrics/panics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int64{
			"total_panics": PanicCount(),
		})
	})

	handler := RecoveryMiddleware(logger)(mux)

	logger.Info("Server listening on :8080")
	http.ListenAndServe(":8080", handler)
}
```

### Step 5: Test

```bash
go run . &
sleep 1

# Normal request
curl -s localhost:8080/api/safe | jq .

# Panic request (should get clean 500)
curl -s localhost:8080/api/panic | jq .

# Another panic
curl -s localhost:8080/api/index | jq .

# Check panic count
curl -s localhost:8080/metrics/panics | jq .

# Wait for background worker to panic and restart
sleep 10
curl -s localhost:8080/metrics/panics | jq .

kill %1
```

---

## Verify

```bash
go build -o server . && ./server > /dev/null 2>&1 &
sleep 1
STATUS=$(curl -s -o /dev/null -w "%{http_code}" localhost:8080/api/panic)
BODY=$(curl -s localhost:8080/api/panic | jq -r .error)
echo "Status: $STATUS"
echo "Body: $BODY"
kill %1
```

Status should be 500, and the body should be `internal server error`.

---

## What's Next

In the final exercise of this section, you will implement blue-green deployment patterns for zero-downtime releases.

## Summary

- Use `defer` + `recover()` in middleware to catch panics without crashing the server
- `runtime/debug.Stack()` captures the stack trace for logging
- Re-panic on `http.ErrAbortHandler` -- it is used intentionally to abort connections
- Wrap the `ResponseWriter` to detect whether headers have been sent before writing error responses
- `SafeGo` wraps goroutines with recovery and optional auto-restart for background workers
- Track panic counts as a metric for alerting

## Reference

- [recover built-in](https://pkg.go.dev/builtin#recover)
- [runtime/debug.Stack](https://pkg.go.dev/runtime/debug#Stack)
- [http.ErrAbortHandler](https://pkg.go.dev/net/http#ErrAbortHandler)
