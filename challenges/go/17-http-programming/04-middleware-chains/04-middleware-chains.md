# 4. Middleware Chains

<!--
difficulty: intermediate
concepts: [middleware, handler-wrapping, logging-middleware, auth-middleware, recovery-middleware, http-handler]
tools: [go, curl]
estimated_time: 30m
bloom_level: apply
prerequisites: [http-server, http-handlefunc, first-class-functions]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - ServeMux Routing and Patterns](../03-servemux-routing-and-patterns/03-servemux-routing-and-patterns.md)
- Understanding of first-class functions and closures

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** middleware functions that wrap `http.Handler`
- **Chain** multiple middleware together in the correct order
- **Implement** logging, authentication, and panic recovery middleware

## Why Middleware

Middleware intercepts HTTP requests before they reach your handler and responses before they reach the client. Common uses include logging, authentication, rate limiting, CORS headers, and panic recovery. Instead of duplicating this logic in every handler, you write it once as middleware and compose it with any handler.

In Go, middleware is a function that takes an `http.Handler` and returns a new `http.Handler`. This pattern lets you stack multiple concerns without modifying the original handler.

## Step 1 -- Create a Logging Middleware

```bash
mkdir -p ~/go-exercises/middleware
cd ~/go-exercises/middleware
go mod init middleware
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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, World!")
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", helloHandler)

	wrapped := loggingMiddleware(mux)

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", wrapped)
}
```

The middleware wraps the entire mux, so every request is logged.

### Intermediate Verification

```bash
curl http://localhost:8080/hello
```

Expected output in terminal: `GET /hello 42.125µs` (time will vary). The response is `Hello, World!`.

## Step 2 -- Add Authentication Middleware

```go
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "Bearer secret-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

### Intermediate Verification

```bash
curl http://localhost:8080/hello
```

Expected: `unauthorized` with status 401.

```bash
curl -H "Authorization: Bearer secret-token" http://localhost:8080/hello
```

Expected: `Hello, World!`

## Step 3 -- Add Recovery Middleware

Catch panics in handlers so the server does not crash:

```go
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered: %v", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func panicHandler(w http.ResponseWriter, r *http.Request) {
	panic("something went wrong")
}
```

### Intermediate Verification

```bash
curl http://localhost:8080/panic
```

Expected: `internal server error` with status 500. Server stays running.

## Step 4 -- Chain Middleware Together

Compose multiple middleware in the correct order:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "Bearer secret-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic recovered: %v", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// chain applies middleware in order: first listed = outermost
func chain(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, World!")
	})

	mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	// Order: recovery -> logging -> auth -> handler
	wrapped := chain(mux, recoveryMiddleware, loggingMiddleware, authMiddleware)

	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", wrapped)
}
```

The `chain` function applies middleware from left to right as the outermost layer. Recovery is outermost so it catches panics in any middleware or handler.

### Intermediate Verification

```bash
curl -H "Authorization: Bearer secret-token" http://localhost:8080/hello
```

Expected: `Hello, World!` and a log line with timing.

```bash
curl -H "Authorization: Bearer secret-token" http://localhost:8080/panic
```

Expected: `internal server error` with status 500, server stays running, log shows recovered panic.

## Step 5 -- Per-Route Middleware

Apply middleware to specific routes instead of the whole mux:

```go
func main() {
	mux := http.NewServeMux()

	publicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Public page")
	})

	privateHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Private page")
	})

	mux.Handle("GET /public", loggingMiddleware(publicHandler))
	mux.Handle("GET /private", loggingMiddleware(authMiddleware(privateHandler)))

	wrapped := recoveryMiddleware(mux)
	fmt.Println("Server starting on :8080")
	http.ListenAndServe(":8080", wrapped)
}
```

### Intermediate Verification

```bash
curl http://localhost:8080/public
```

Expected: `Public page` (no auth required).

```bash
curl http://localhost:8080/private
```

Expected: `unauthorized` (auth middleware kicks in).

## Common Mistakes

### Wrong Middleware Order

**Wrong:** Putting auth before recovery means a panic in auth crashes the server.

**Fix:** Recovery should always be the outermost middleware.

### Not Calling next.ServeHTTP

**Wrong:**

```go
func myMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("before")
		// forgot to call next.ServeHTTP(w, r)
	})
}
```

**What happens:** The request never reaches the handler. The response is empty.

### Modifying the Request After Calling Next

The handler has already used the request by the time `next.ServeHTTP` returns. To modify the request, do it before calling next. To modify the response, wrap the `ResponseWriter`.

## Verify What You Learned

Test the chained server with all combinations:

```bash
curl http://localhost:8080/hello
curl -H "Authorization: Bearer secret-token" http://localhost:8080/hello
curl -H "Authorization: Bearer secret-token" http://localhost:8080/panic
```

Confirm: unauthorized without token, success with token, panic is recovered.

## What's Next

Continue to [05 - Request Body Parsing and Validation](../05-request-body-parsing-and-validation/05-request-body-parsing-and-validation.md) to learn how to parse and validate JSON request bodies.

## Summary

- Middleware is a function `func(http.Handler) http.Handler` that wraps a handler
- Use `http.HandlerFunc` to convert a function to an `http.Handler`
- Chain middleware by nesting calls or using a helper function
- Recovery middleware should be outermost, logging next, then auth
- Apply middleware per-route or globally on the mux

## Reference

- [http.Handler interface](https://pkg.go.dev/net/http#Handler)
- [http.HandlerFunc](https://pkg.go.dev/net/http#HandlerFunc)
- [Making a RESTful JSON API in Go](https://go.dev/doc/tutorial/web-service-gin)
