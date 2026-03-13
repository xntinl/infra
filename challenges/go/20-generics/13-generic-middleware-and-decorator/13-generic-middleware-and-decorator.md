# 13. Generic Middleware and Decorator

<!--
difficulty: insane
concepts: [middleware-pattern, decorator-pattern, function-composition, generic-pipelines, type-safe-wrapping]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [type-parameters, closures, interface-constraints-with-methods, generics-vs-interfaces, context]
-->

## The Challenge

Design and implement a generic middleware/decorator system in Go. The system must support type-safe function wrapping where the middleware can inspect, modify, or short-circuit requests and responses without losing type information.

Traditional Go middleware (like `func(http.Handler) http.Handler`) works well for HTTP but is not reusable for other domains -- database queries, message processing, RPC calls. Your system must generalize the middleware pattern so it works for any request/response pair.

This is a design challenge. You will make architectural decisions about how to represent middleware generically, how to compose chains, and how to handle cross-cutting concerns like logging, timing, retries, and caching.

## Requirements

### Core Middleware System

1. Define a generic `Handler[Req, Resp any]` type representing a function that processes a request and returns a response:
   ```go
   type Handler[Req, Resp any] func(ctx context.Context, req Req) (Resp, error)
   ```

2. Define a generic `Middleware[Req, Resp any]` type that wraps a handler:
   ```go
   type Middleware[Req, Resp any] func(Handler[Req, Resp]) Handler[Req, Resp]
   ```

3. Implement `Chain[Req, Resp any](handler Handler[Req, Resp], mw ...Middleware[Req, Resp]) Handler[Req, Resp]` that composes middleware in order (first middleware is outermost).

### Built-in Middleware

Implement these generic middleware functions:

4. `WithLogging[Req, Resp any](logger *slog.Logger) Middleware[Req, Resp]` -- logs request start and completion with duration
5. `WithTimeout[Req, Resp any](d time.Duration) Middleware[Req, Resp]` -- adds a timeout to the context
6. `WithRetry[Req, Resp any](maxAttempts int, backoff time.Duration, isRetryable func(error) bool) Middleware[Req, Resp]` -- retries on retryable errors
7. `WithCache[Req comparable, Resp any](ttl time.Duration) Middleware[Req, Resp]` -- caches responses by request (note the `comparable` constraint on Req)

### Demonstration

8. Create two different handler types:
   - A `UserService` handler: `Handler[GetUserReq, User]`
   - A `MathService` handler: `Handler[CalcReq, CalcResp]`

9. Apply the middleware chain to both handler types, showing that the same middleware works across different domains.

## Hints

<details>
<summary>Hint 1: Chain implementation</summary>

Apply middleware in reverse order so the first middleware in the list is the outermost wrapper:

```go
func Chain[Req, Resp any](handler Handler[Req, Resp], mw ...Middleware[Req, Resp]) Handler[Req, Resp] {
    for i := len(mw) - 1; i >= 0; i-- {
        handler = mw[i](handler)
    }
    return handler
}
```
</details>

<details>
<summary>Hint 2: Generic logging middleware</summary>

```go
func WithLogging[Req, Resp any](logger *slog.Logger) Middleware[Req, Resp] {
    return func(next Handler[Req, Resp]) Handler[Req, Resp] {
        return func(ctx context.Context, req Req) (Resp, error) {
            start := time.Now()
            logger.Info("request started", "request", req)
            resp, err := next(ctx, req)
            logger.Info("request completed",
                "duration_ms", time.Since(start).Milliseconds(),
                "error", err,
            )
            return resp, err
        }
    }
}
```
</details>

<details>
<summary>Hint 3: Generic cache middleware with comparable constraint</summary>

The cache middleware requires `Req` to be `comparable` so it can be used as a map key:

```go
func WithCache[Req comparable, Resp any](ttl time.Duration) Middleware[Req, Resp] {
    type entry struct {
        resp   Resp
        expiry time.Time
    }
    var mu sync.RWMutex
    cache := make(map[Req]entry)

    return func(next Handler[Req, Resp]) Handler[Req, Resp] {
        return func(ctx context.Context, req Req) (Resp, error) {
            mu.RLock()
            if e, ok := cache[req]; ok && time.Now().Before(e.expiry) {
                mu.RUnlock()
                return e.resp, nil
            }
            mu.RUnlock()

            resp, err := next(ctx, req)
            if err == nil {
                mu.Lock()
                cache[req] = entry{resp: resp, expiry: time.Now().Add(ttl)}
                mu.Unlock()
            }
            return resp, err
        }
    }
}
```
</details>

<details>
<summary>Hint 4: Making it work across domains</summary>

The key insight is that `Handler[Req, Resp]` and `Middleware[Req, Resp]` are parameterized by the request/response types. The same `WithLogging` function generates middleware for `Handler[GetUserReq, User]` and `Handler[CalcReq, CalcResp]` through type inference:

```go
userHandler := Chain(
    getUserHandler,
    WithLogging[GetUserReq, User](logger),
    WithTimeout[GetUserReq, User](5*time.Second),
)

mathHandler := Chain(
    calcHandler,
    WithLogging[CalcReq, CalcResp](logger),
    WithRetry[CalcReq, CalcResp](3, 100*time.Millisecond, isTransient),
)
```
</details>

## Success Criteria

- [ ] `Handler[Req, Resp]` and `Middleware[Req, Resp]` are fully generic types
- [ ] `Chain` composes multiple middleware in the correct order (first is outermost)
- [ ] `WithLogging` logs request start and completion with duration for any handler type
- [ ] `WithTimeout` cancels the context after the specified duration
- [ ] `WithRetry` retries retryable errors with backoff, does not retry permanent errors
- [ ] `WithCache` caches responses by request value, respects TTL expiration
- [ ] The same middleware functions work with two different handler types in the demo
- [ ] The program compiles and demonstrates all middleware behaviors
- [ ] No `interface{}` or type assertions -- everything is compile-time type-safe

## Research Resources

- [Go blog: When to Use Generics](https://go.dev/blog/when-generics) -- guidance on appropriate generic usage
- [Middleware pattern in Go](https://pkg.go.dev/net/http#Handler) -- the standard HTTP middleware pattern this generalizes
- [Functional options pattern](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis) -- related composition pattern
- [gRPC interceptors](https://grpc.io/docs/guides/interceptors/) -- another domain-specific middleware system that could be generalized
