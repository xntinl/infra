# 62. Middleware Pipeline for HTTP

<!--
difficulty: intermediate-advanced
category: web-servers-and-http
languages: [go]
concepts: [middleware, function-composition, decorator-pattern, context-propagation, onion-model, request-lifecycle]
estimated_time: 3-4 hours
bloom_level: apply
prerequisites: [go-basics, closures, http-handler-interface, context-package, error-handling]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Closures and higher-order functions in Go
- The `http.Handler` and `http.HandlerFunc` interfaces
- `context.Context` for value propagation and deadlines
- Panic recovery with `recover()` in deferred functions
- HTTP headers: CORS, `X-Request-ID`, `Content-Type`

## Learning Objectives

- **Apply** the onion model of middleware where each layer wraps the next handler
- **Implement** reusable middleware components (logging, recovery, CORS, request ID, timeout)
- **Design** a pipeline builder that supports global, per-route, and conditional middleware
- **Analyze** how middleware ordering affects request/response processing flow
- **Evaluate** the impact of context propagation on request-scoped data across middleware layers

## The Challenge

Middleware is the spine of every HTTP server. Before your handler runs, you need to log the request, assign a unique ID, enforce timeouts, recover from panics, and set CORS headers. After the handler runs, you need to log the response status and duration. Each of these concerns should be independent, composable, and reorderable.

Your task is to build a middleware pipeline framework. The core abstraction is simple: a middleware is a function that takes a handler and returns a new handler. The pipeline chains these functions in order, creating an onion where the outermost middleware runs first on the request and last on the response. Beyond the core, you will build five production-grade middleware components and support three composition modes: global middleware (applied to all routes), per-route middleware (applied to specific routes), and conditional middleware (applied only when a predicate is true).

The pipeline must use `context.Context` for passing request-scoped data (request ID, start time, user info) between middleware layers and handlers, without global state.

## Requirements

1. Define `Middleware` as `func(Handler) Handler` where `Handler` is your own interface (not `net/http`) with a `ServeHTTP(ResponseWriter, *Request)` method
2. Implement a `Pipeline` builder with `Use(middleware...)` for global middleware and `Build(handler) Handler` to produce the final wrapped handler
3. Build **logging middleware**: logs method, path, status code, and duration for every request
4. Build **panic recovery middleware**: catches panics in downstream handlers, logs the stack trace, and returns a 500 response
5. Build **CORS middleware**: configurable allowed origins, methods, and headers; handles preflight `OPTIONS` requests automatically
6. Build **request ID middleware**: generates a UUID for each request, sets it in context and in the `X-Request-ID` response header
7. Build **timeout middleware**: wraps the handler in a `context.WithTimeout`; if the handler exceeds the deadline, returns 503 Service Unavailable
8. Support per-route middleware: `Pipeline.Route(pattern, handler, middleware...)` applies route-specific middleware after global middleware
9. Support conditional middleware: `When(predicate, middleware)` applies the middleware only when `predicate(request)` returns true
10. Middleware ordering must be deterministic: the first middleware added via `Use()` is the outermost wrapper

## Hints

<details>
<summary>Hint 1: The onion model composition</summary>

Chaining middleware is just function composition in reverse order. If you have middlewares [A, B, C] and a handler H, the result is A(B(C(H))). Build from the inside out:

```go
final := handler
for i := len(middlewares) - 1; i >= 0; i-- {
    final = middlewares[i](final)
}
```

A request flows: A.before -> B.before -> C.before -> H -> C.after -> B.after -> A.after.
</details>

<details>
<summary>Hint 2: Capturing response status for logging</summary>

The standard `ResponseWriter` does not expose the status code after `WriteHeader` is called. Wrap it in a struct that intercepts `WriteHeader` and stores the code:

```go
type statusRecorder struct {
    http.ResponseWriter
    statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
    r.statusCode = code
    r.ResponseWriter.WriteHeader(code)
}
```
</details>

<details>
<summary>Hint 3: Context values for request-scoped data</summary>

Use typed keys to avoid collisions in `context.WithValue`:

```go
type contextKey string
const requestIDKey contextKey = "request_id"

ctx := context.WithValue(r.Context(), requestIDKey, uuid)
r = r.WithContext(ctx)
```

Downstream middleware and handlers retrieve the value with `r.Context().Value(requestIDKey)`.
</details>

<details>
<summary>Hint 4: Timeout middleware with context cancellation</summary>

Wrap the handler in a goroutine, then select on the context deadline or the handler completion. If the deadline fires first, write a 503 response. Be careful: the goroutine may still try to write to the ResponseWriter after timeout -- use a sync.Once or a flag to prevent double writes.
</details>

## Acceptance Criteria

- [ ] Middleware chain executes in the correct onion order (outermost first on request, last on response)
- [ ] Logging middleware prints method, path, status, and duration for every request
- [ ] Panic recovery catches panics, logs the error, and returns 500 without crashing the server
- [ ] CORS middleware handles preflight requests and sets correct headers
- [ ] Request ID middleware generates unique IDs accessible via context and response header
- [ ] Timeout middleware returns 503 when the handler exceeds the configured deadline
- [ ] Per-route middleware applies only to the specified route
- [ ] Conditional middleware executes only when the predicate is true
- [ ] Middleware ordering is deterministic and matches the order of `Use()` calls
- [ ] All middleware is stateless and safe for concurrent use

## Research Resources

- [Go Blog: The http.Handler interface](https://go.dev/blog/http-handler) -- foundational Handler/HandlerFunc patterns
- [Mat Ryer: Writing middleware in #golang](https://medium.com/@matryer/writing-middleware-in-golang-and-how-go-makes-it-so-much-fun-4375c1246e81) -- practical middleware composition patterns
- [Alice: Painless middleware chaining for Go](https://github.com/justinas/alice) -- minimal middleware chaining library; study the source (~100 lines)
- [Chi Router: Middleware design](https://github.com/go-chi/chi) -- production middleware patterns with per-route composition
- [MDN: CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS) -- complete CORS specification for implementing the middleware
- [Go Concurrency Patterns: Context](https://go.dev/blog/context) -- context propagation and cancellation
