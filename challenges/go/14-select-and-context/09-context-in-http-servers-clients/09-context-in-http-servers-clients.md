# 9. Context in HTTP Servers and Clients

<!--
difficulty: advanced
concepts: [http-context, request-context, client-timeout, server-cancellation, context-middleware]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [context-withcancel, context-withtimeout-withdeadline, context-propagation, http-server-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Context Propagation](../07-context-propagation/07-context-propagation.md)
- Familiarity with `net/http` basics (Section 17)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how `net/http` attaches a context to every incoming request
- **Apply** request contexts in HTTP handlers for downstream cancellation
- **Implement** HTTP clients that propagate context for timeout and cancellation
- **Design** middleware that enriches the request context

## The Problem

HTTP servers must handle client disconnects gracefully. If a client closes the connection mid-request, the server should stop doing expensive work (database queries, external API calls) rather than wasting resources on a result nobody will receive.

Go's `net/http` package integrates context deeply: every `*http.Request` carries a context (`r.Context()`) that is cancelled when the client disconnects, the server shuts down, or a handler-level timeout fires. On the client side, `http.NewRequestWithContext` lets you attach timeouts and cancellation to outgoing requests.

Your task: build an HTTP server and client that properly propagate context through the entire request lifecycle.

## Step 1 -- Server Request Context

```bash
mkdir -p ~/go-exercises/http-context && cd ~/go-exercises/http-context
go mod init http-context
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func slowHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log.Println("handler: started")

	select {
	case <-time.After(5 * time.Second):
		fmt.Fprintln(w, "completed")
		log.Println("handler: completed")
	case <-ctx.Done():
		log.Println("handler: client disconnected:", ctx.Err())
		// No need to write a response -- client is gone
	}
}

func main() {
	http.HandleFunc("/slow", slowHandler)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### Intermediate Verification

```bash
go run main.go &
# In another terminal, start a request and cancel it with Ctrl+C after 1 second:
curl http://localhost:8080/slow
# Or use timeout:
timeout 1 curl http://localhost:8080/slow
kill %1
```

When the client disconnects, the handler detects it through `ctx.Done()`.

## Step 2 -- Client with Context Timeout

Build a client that cancels requests after a deadline:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/fast", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintln(w, "fast response")
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			fmt.Fprintln(w, "slow response")
		case <-r.Context().Done():
			log.Println("server: client cancelled")
		}
	})
	go http.ListenAndServe(":8081", mux)
	time.Sleep(50 * time.Millisecond) // wait for server to start
}

func fetchWithTimeout(url string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("request error: %v\n", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("fetch %s: %v\n", url, err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("fetch %s: %s", url, body)
}

func main() {
	startServer()

	fetchWithTimeout("http://localhost:8081/fast", 500*time.Millisecond)
	fetchWithTimeout("http://localhost:8081/slow", 500*time.Millisecond)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
fetch http://localhost:8081/fast: fast response
... fetch http://localhost:8081/slow: Get "http://localhost:8081/slow": context deadline exceeded
server: client cancelled
```

## Step 3 -- Propagate Context Through Service Layers

Pass the request context through service and repository layers so that a client disconnect cancels database queries:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Simulated database query
func queryDB(ctx context.Context, query string) (string, error) {
	select {
	case <-time.After(300 * time.Millisecond):
		return "db-result-for-" + query, nil
	case <-ctx.Done():
		return "", fmt.Errorf("query cancelled: %w", ctx.Err())
	}
}

// Service layer
func getUser(ctx context.Context, id string) (string, error) {
	return queryDB(ctx, "user-"+id)
}

// Handler
func userHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.URL.Query().Get("id")
	if userID == "" {
		userID = "1"
	}

	result, err := getUser(ctx, userID)
	if err != nil {
		log.Printf("handler: %v\n", err)
		http.Error(w, "request cancelled", http.StatusServiceUnavailable)
		return
	}

	fmt.Fprintln(w, result)
}

func main() {
	http.HandleFunc("/user", userHandler)
	log.Println("listening on :8082")
	log.Fatal(http.ListenAndServe(":8082", nil))
}
```

Test by starting the server, fetching `/user?id=42`, and cancelling the request before 300ms.

## Step 4 -- Handler Timeout Middleware

Write middleware that wraps handlers with a timeout:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

func withTimeout(timeout time.Duration, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

func expensiveHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Simulate expensive work
	select {
	case <-time.After(2 * time.Second):
		fmt.Fprintln(w, "done")
	case <-ctx.Done():
		log.Println("handler: timed out:", ctx.Err())
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/work", withTimeout(500*time.Millisecond, expensiveHandler))
	log.Println("listening on :8083")
	log.Fatal(http.ListenAndServe(":8083", mux))
}
```

### Intermediate Verification

```bash
go run main.go &
curl http://localhost:8083/work
kill %1
```

Expected: the client receives "timeout" after 500ms.

## Step 5 -- Context-Enriching Middleware

Add request ID and authentication info to the context via middleware:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	userKey
)

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("req-%06d", rand.Intn(1000000))
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-User")
		if user == "" {
			user = "anonymous"
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func handler(w http.ResponseWriter, r *http.Request) {
	reqID := r.Context().Value(requestIDKey).(string)
	user := r.Context().Value(userKey).(string)
	msg := fmt.Sprintf("[%s] hello, %s", reqID, user)
	log.Println(msg)
	fmt.Fprintln(w, msg)
}

func main() {
	h := requestIDMiddleware(authMiddleware(http.HandlerFunc(handler)))
	http.Handle("/hello", h)
	log.Println("listening on :8084")
	log.Fatal(http.ListenAndServe(":8084", nil))
}
```

## Common Mistakes

### Ignoring r.Context()

Every `*http.Request` already has a context. Ignoring it and creating `context.Background()` breaks cancellation when clients disconnect.

### Writing Response After Context Cancellation

After `ctx.Done()` fires because the client disconnected, writing to `http.ResponseWriter` may panic or silently fail. Check context before writing.

### Not Using http.NewRequestWithContext

The zero-value `http.Request` has `context.Background()`. Always use `http.NewRequestWithContext` to attach deadlines to outgoing requests.

## Verify What You Learned

Build an HTTP server with:
1. A `/search` endpoint that queries three simulated backends concurrently
2. If any backend is slow (> 200ms), cancel the others using shared context
3. Return the first result to the client
4. Add a timeout middleware that limits total handler time to 500ms
5. Test with `curl` and verify cancellation messages in the server logs

## What's Next

Continue to [10 - Context-Aware Database Queries](../10-context-aware-database-queries/10-context-aware-database-queries.md) to see how context integrates with `database/sql`.

## Summary

- Every `*http.Request` carries a context via `r.Context()`
- The request context is cancelled when the client disconnects or the server shuts down
- Use `r.WithContext(ctx)` to attach enriched or timeout-wrapped contexts
- `http.NewRequestWithContext` attaches context to outgoing client requests
- Middleware can add timeouts, request IDs, and auth data to the context
- Always check `ctx.Done()` before doing expensive work in handlers

## Reference

- [http.Request.Context](https://pkg.go.dev/net/http#Request.Context)
- [http.NewRequestWithContext](https://pkg.go.dev/net/http#NewRequestWithContext)
- [http.TimeoutHandler](https://pkg.go.dev/net/http#TimeoutHandler)
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
