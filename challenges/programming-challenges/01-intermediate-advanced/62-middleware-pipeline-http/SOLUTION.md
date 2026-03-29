# Solution: Middleware Pipeline for HTTP

## Architecture Overview

The pipeline is built on three abstractions: a **Handler** interface that mirrors `http.Handler` but uses our own Request/ResponseWriter types, a **Middleware** function type that wraps one Handler into another, and a **Pipeline** builder that composes middleware in onion order.

Each middleware is a pure function: no shared mutable state, no globals. Request-scoped data flows through `context.Context`. The pipeline builds from the inside out: the last middleware added via `Use()` is the innermost wrapper, so the first one added is the outermost and runs first on the request, last on the response.

```
Request flow:         Response flow:
Logging    ------>    Logging (logs duration + status)
  Recovery  ----->      Recovery (catches panics)
    CORS     ---->        CORS (sets headers)
      ReqID   --->          ReqID
        Timeout -->           Timeout
          Handler               Handler
```

## Go Solution

### Project Setup

```bash
mkdir -p middleware-pipeline && cd middleware-pipeline
go mod init middleware-pipeline
```

### Implementation

```go
// middleware.go
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Middleware wraps a handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// Pipeline composes middleware in a deterministic order.
type Pipeline struct {
	global []Middleware
	routes map[string]routeEntry
}

type routeEntry struct {
	handler    http.Handler
	middleware []Middleware
}

// New creates an empty pipeline.
func New() *Pipeline {
	return &Pipeline{
		routes: make(map[string]routeEntry),
	}
}

// Use appends global middleware. The first added is the outermost wrapper.
func (p *Pipeline) Use(mw ...Middleware) *Pipeline {
	p.global = append(p.global, mw...)
	return p
}

// Route registers a handler with optional per-route middleware.
func (p *Pipeline) Route(pattern string, handler http.Handler, mw ...Middleware) {
	p.routes[pattern] = routeEntry{handler: handler, middleware: mw}
}

// Build wraps the given handler with all global middleware.
func (p *Pipeline) Build(handler http.Handler) http.Handler {
	return chain(p.global, handler)
}

// BuildMux produces an http.Handler that dispatches registered routes,
// each wrapped in global + per-route middleware.
func (p *Pipeline) BuildMux() http.Handler {
	mux := http.NewServeMux()
	for pattern, entry := range p.routes {
		// Per-route middleware wraps the handler first, then global wraps the result
		h := chain(entry.middleware, entry.handler)
		h = chain(p.global, h)
		mux.Handle(pattern, h)
	}
	return mux
}

func chain(middlewares []Middleware, handler http.Handler) http.Handler {
	final := handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		final = middlewares[i](final)
	}
	return final
}

// When applies a middleware only when the predicate returns true for the request.
func When(pred func(*http.Request) bool, mw Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pred(r) {
				wrapped.ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// --- StatusRecorder captures the HTTP status code ---

type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.statusCode = code
		r.wroteHeader = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// --- Logging Middleware ---

func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := newStatusRecorder(w)

			next.ServeHTTP(rec, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.statusCode,
				"duration", time.Since(start),
				"request_id", requestIDFromContext(r.Context()),
			)
		})
	}
}

// --- Panic Recovery Middleware ---

func Recovery(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					stack := debug.Stack()
					logger.Error("panic recovered",
						"error", fmt.Sprintf("%v", err),
						"stack", string(stack),
						"path", r.URL.Path,
					)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// --- CORS Middleware ---

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int // seconds
}

func CORS(cfg CORSConfig) Middleware {
	allowedOrigins := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowedOrigins[o] = true
	}
	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	maxAge := fmt.Sprintf("%d", cfg.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowedOrigins["*"] || allowedOrigins[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				w.Header().Set("Access-Control-Max-Age", maxAge)
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// --- Request ID Middleware ---

type contextKey string

const reqIDKey contextKey = "request_id"

func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = generateID()
			}

			ctx := context.WithValue(r.Context(), reqIDKey, id)
			r = r.WithContext(ctx)

			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r)
		})
	}
}

func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(reqIDKey).(string)
	return id
}

// RequestIDFromContext exports access to the request ID.
func RequestIDFromContext(ctx context.Context) string {
	return requestIDFromContext(ctx)
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Timeout Middleware ---

func Timeout(duration time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			r = r.WithContext(ctx)

			done := make(chan struct{})
			tw := &timeoutWriter{
				ResponseWriter: w,
			}

			go func() {
				next.ServeHTTP(tw, r)
				close(done)
			}()

			select {
			case <-done:
				// Handler completed in time
			case <-ctx.Done():
				tw.mu.Lock()
				defer tw.mu.Unlock()
				if !tw.wroteHeader {
					tw.timedOut = true
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				}
			}
		})
	}
}

type timeoutWriter struct {
	http.ResponseWriter
	mu          sync.Mutex
	wroteHeader bool
	timedOut    bool
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return
	}
	tw.wroteHeader = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, context.DeadlineExceeded
	}
	tw.wroteHeader = true
	return tw.ResponseWriter.Write(b)
}
```

### Tests

```go
// middleware_test.go
package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestOnionOrder(t *testing.T) {
	var order []string

	mkMiddleware := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+"-before")
				next.ServeHTTP(w, r)
				order = append(order, name+"-after")
			})
		}
	}

	p := New()
	p.Use(mkMiddleware("A"), mkMiddleware("B"), mkMiddleware("C"))

	handler := p.Build(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := []string{"A-before", "B-before", "C-before", "handler", "C-after", "B-after", "A-after"}
	if len(order) != len(expected) {
		t.Fatalf("got %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestLogging(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest("POST", "/api/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, "POST") {
		t.Error("log should contain method")
	}
	if !strings.Contains(output, "/api/users") {
		t.Error("log should contain path")
	}
	if !strings.Contains(output, "201") {
		t.Error("log should contain status code 201")
	}
}

func TestPanicRecovery(t *testing.T) {
	logger := testLogger()

	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/crash", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	cors := CORS(CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		MaxAge:         3600,
	})

	handler := cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	t.Run("preflight", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("preflight status = %d, want 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
			t.Errorf("ACAO = %q, want %q", got, "https://example.com")
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api", nil)
		req.Header.Set("Origin", "https://evil.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("ACAO should be empty for disallowed origin, got %q", got)
		}
	})
}

func TestRequestID(t *testing.T) {
	var capturedID string

	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestIDFromContext(r.Context())
		w.WriteHeader(200)
	}))

	t.Run("generates ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		headerID := rec.Header().Get("X-Request-ID")
		if headerID == "" {
			t.Error("X-Request-ID header should be set")
		}
		if capturedID == "" {
			t.Error("request ID should be in context")
		}
		if headerID != capturedID {
			t.Error("header and context IDs should match")
		}
	})

	t.Run("preserves existing ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", "existing-123")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if capturedID != "existing-123" {
			t.Errorf("should preserve existing ID, got %q", capturedID)
		}
	})
}

func TestTimeout(t *testing.T) {
	t.Run("handler completes in time", func(t *testing.T) {
		handler := Timeout(100 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Write([]byte("ok"))
		}))

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != 200 {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("handler exceeds deadline", func(t *testing.T) {
		handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(200)
		}))

		req := httptest.NewRequest("GET", "/slow", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rec.Code)
		}
	})
}

func TestConditionalMiddleware(t *testing.T) {
	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	onlyPOST := When(func(r *http.Request) bool {
		return r.Method == "POST"
	}, mw)

	handler := onlyPOST(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	t.Run("predicate true", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("POST", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if !called {
			t.Error("middleware should run when predicate is true")
		}
	})

	t.Run("predicate false", func(t *testing.T) {
		called = false
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if called {
			t.Error("middleware should NOT run when predicate is false")
		}
	})
}

func TestPerRouteMiddleware(t *testing.T) {
	adminCalled := false
	adminMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminCalled = true
			next.ServeHTTP(w, r)
		})
	}

	p := New()
	p.Use(RequestID())
	p.Route("/admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}), adminMW)
	p.Route("/public", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	mux := p.BuildMux()

	t.Run("admin route has admin middleware", func(t *testing.T) {
		adminCalled = false
		req := httptest.NewRequest("GET", "/admin", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if !adminCalled {
			t.Error("admin middleware should run on /admin")
		}
	})

	t.Run("public route skips admin middleware", func(t *testing.T) {
		adminCalled = false
		req := httptest.NewRequest("GET", "/public", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if adminCalled {
			t.Error("admin middleware should NOT run on /public")
		}
	})
}

func TestFullPipeline(t *testing.T) {
	logger := testLogger()

	p := New()
	p.Use(
		Recovery(logger),
		RequestID(),
		Logging(logger),
		Timeout(5*time.Second),
		CORS(CORSConfig{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST"},
			AllowedHeaders: []string{"Content-Type"},
			MaxAge:         3600,
		}),
	)

	handler := p.Build(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"request_id":"` + id + `"}`))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header should be set")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("CORS header should be set")
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestOnionOrder
--- PASS: TestOnionOrder (0.00s)
=== RUN   TestLogging
--- PASS: TestLogging (0.00s)
=== RUN   TestPanicRecovery
--- PASS: TestPanicRecovery (0.00s)
=== RUN   TestCORS
=== RUN   TestCORS/preflight
=== RUN   TestCORS/disallowed_origin
--- PASS: TestCORS (0.00s)
=== RUN   TestRequestID
=== RUN   TestRequestID/generates_ID
=== RUN   TestRequestID/preserves_existing_ID
--- PASS: TestRequestID (0.00s)
=== RUN   TestTimeout
=== RUN   TestTimeout/handler_completes_in_time
=== RUN   TestTimeout/handler_exceeds_deadline
--- PASS: TestTimeout (0.25s)
=== RUN   TestConditionalMiddleware
=== RUN   TestConditionalMiddleware/predicate_true
=== RUN   TestConditionalMiddleware/predicate_false
--- PASS: TestConditionalMiddleware (0.00s)
=== RUN   TestPerRouteMiddleware
=== RUN   TestPerRouteMiddleware/admin_route_has_admin_middleware
=== RUN   TestPerRouteMiddleware/public_route_skips_admin_middleware
--- PASS: TestPerRouteMiddleware (0.00s)
=== RUN   TestFullPipeline
--- PASS: TestFullPipeline (0.00s)
PASS
```

## Design Decisions

**Decision 1: Using `http.Handler` interface vs. custom Handler type.** The solution uses Go's standard `http.Handler` and `http.HandlerFunc` for maximum compatibility with existing ecosystem. The pipeline, middleware, and composed handlers all work directly with `net/http`, meaning any third-party middleware that follows the same convention is plug-compatible. A custom Handler type would require adapters for every external middleware.

**Decision 2: Status recorder via composition over interface assertion.** The `statusRecorder` embeds `http.ResponseWriter` and intercepts `WriteHeader`. This is simpler than type-asserting for interfaces like `http.Flusher` or `http.Hijacker`. The trade-off is that advanced response features are hidden behind the wrapper. Production middleware often uses `httpsnoop` to preserve all interfaces.

**Decision 3: Timeout middleware uses a goroutine and channel.** The handler runs in a separate goroutine, and the middleware selects between completion and the context deadline. The `timeoutWriter` with a mutex prevents the handler goroutine from writing after timeout. An alternative is `http.TimeoutHandler` from the standard library, but building it from scratch reveals the synchronization complexity.

**Decision 4: Conditional middleware evaluated at request time.** The `When()` wrapper evaluates its predicate on every request. An alternative is to build separate pipelines for different conditions, but that complicates the builder API. The per-request check is cheap (one function call) and keeps the API simple.

## Common Mistakes

**Mistake 1: Reversing the middleware chain direction.** If you compose `[A, B, C]` by iterating forward (`A(B(C(H)))`), A is outermost. If you iterate backward and apply incorrectly, you get `C(B(A(H)))`. The solution iterates from the last middleware to the first, applying each to the accumulated handler.

**Mistake 2: Writing to ResponseWriter after timeout.** The handler goroutine continues running after the timeout middleware returns 503. If the handler then calls `w.Write()`, it corrupts the response or panics on a closed connection. The `timeoutWriter` mutex prevents this, but forgetting the guard is a common source of data races.

**Mistake 3: Not setting default status code in the recorder.** If the handler calls `w.Write()` without `w.WriteHeader()`, Go's default is 200. The `statusRecorder` must initialize `statusCode` to 200 to match this implicit behavior. Initializing to 0 produces incorrect logs.

**Mistake 4: Global state in middleware.** Storing request counts or error rates in package-level variables creates data races under concurrent requests. All state must be either in the closure (configured once at construction) or in the request context.

## Performance Notes

- Each middleware adds one function call and one stack frame per request. For a pipeline of 5 middleware, this is 10 extra function calls (5 before handler, 5 after). The overhead is negligible compared to any I/O the handler performs.
- The `statusRecorder` allocation can be pooled with `sync.Pool` if profiling shows GC pressure from high-throughput endpoints.
- The timeout middleware's goroutine-per-request adds ~4KB of stack space. For endpoints that are known to be fast, skip the timeout middleware to avoid this cost.
- Context value lookups are O(depth) where depth is the number of `WithValue` calls in the chain. Keep the number of context values small (request ID, user, deadline) to avoid degradation.
