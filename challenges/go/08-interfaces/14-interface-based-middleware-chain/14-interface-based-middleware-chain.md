# 14. Interface-Based Middleware Chain

<!--
difficulty: insane
concepts: [middleware-pattern, handler-wrapping, interface-composition, decorator-chain, context-propagation, type-safe-middleware]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [implicit-interface-satisfaction, interface-composition, dependency-injection, common-standard-library-interfaces]
-->

## Prerequisites

- Completed exercises 1-12 in this section or equivalent experience
- Strong understanding of `http.Handler` and `http.HandlerFunc`
- Familiarity with the decorator pattern and function composition
- Experience with `context.Context` for request-scoped data

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a type-safe middleware chain system that goes beyond simple `func(http.Handler) http.Handler` composition
- **Design** middleware interfaces that support pre-processing, post-processing, error handling, and short-circuiting
- **Evaluate** the tradeoffs between function-based and interface-based middleware approaches

## The Challenge

Build a middleware framework that uses interfaces rather than function closures to define middleware behavior. While the standard `func(http.Handler) http.Handler` pattern is simple, it has limitations: middleware cannot easily share typed data with downstream handlers, error handling requires convention rather than types, and the execution order (pre/post processing) is implicit in closure nesting.

Your framework defines explicit interfaces for different middleware concerns: `PreProcessor` runs before the handler and can short-circuit, `PostProcessor` runs after the handler and can inspect/modify the response, `ErrorHandler` catches panics and handler errors, and `ResponseTransformer` can modify the response body before it is written. Each middleware implements one or more of these interfaces, and the chain orchestrator calls them in the correct order.

The hardest part is response interception. The standard `http.ResponseWriter` writes directly to the client — once `WriteHeader` is called, there is no going back. Your framework must use a buffered response writer that captures the response so PostProcessors and ResponseTransformers can inspect and modify it before the final write. This introduces memory and latency tradeoffs that your design must address.

Additionally, your middleware chain must support typed context injection. Instead of using `context.WithValue` with arbitrary keys (which loses type safety), define a `ContextInjector` interface that lets middleware declare what typed values they add to the context, and a corresponding `ContextConsumer` interface that declares what values downstream middleware or handlers require. The chain builder should validate at construction time that all required context values are provided by upstream middleware.

## Requirements

1. Define these middleware interfaces: `PreProcessor` (receives request, returns modified request or error to short-circuit), `PostProcessor` (receives request and captured response, can modify response), `ErrorHandler` (receives the error from handler or panic recovery), `ResponseTransformer` (receives response body bytes and headers, returns transformed body)

2. Implement a `Chain` builder that accepts middleware in order and validates the chain at build time — not at request time

3. Implement a `BufferedResponseWriter` that captures status code, headers, and body, allowing PostProcessors and ResponseTransformers to inspect and modify the response before it is flushed to the real `http.ResponseWriter`

4. Support short-circuiting: if a PreProcessor returns an error or a response, the handler and subsequent PreProcessors are skipped, but PostProcessors still run (for logging, metrics, etc.)

5. Implement typed context injection: middleware implementing `ContextInjector` declares a type key and provides a value; middleware implementing `ContextConsumer` declares required type keys — the chain builder rejects chains where a consumer's requirements are not met by an upstream injector

6. The chain must handle panics in handlers and middleware, routing them to ErrorHandler middleware rather than crashing the server

7. Implement at least 5 concrete middleware: `RequestLogger` (PreProcessor + PostProcessor), `Authentication` (PreProcessor + ContextInjector for user identity), `RateLimiter` (PreProcessor with short-circuit), `ResponseCompressor` (ResponseTransformer using gzip), `PanicRecovery` (ErrorHandler)

8. Support middleware priority/ordering constraints: middleware can declare `Before() []string` and `After() []string` to specify ordering relative to other named middleware — the chain builder resolves these constraints or returns an error

9. Implement streaming support: for large responses, the `ResponseTransformer` should be able to opt into streaming mode where it receives an `io.Reader` instead of buffering the entire body

10. Write benchmarks comparing your interface-based chain against the standard `func(http.Handler) http.Handler` closure chain for chains of 1, 5, and 10 middleware — document the overhead

## Hints

- The `BufferedResponseWriter` needs to implement `http.ResponseWriter`, `http.Flusher`, and `http.Hijacker` — but only delegate `Flusher` and `Hijacker` if the underlying writer supports them (use type assertion)

- For typed context injection, consider using generics: `ContextKey[T]` as a typed key that erases to a common interface for the chain builder's validation, but provides type-safe `Get(ctx context.Context) (T, bool)` for consumers

- Short-circuit semantics: define a `ShortCircuitResponse` type that PreProcessors return to indicate "stop processing, use this response" — this is cleaner than returning an error for non-error short-circuits (like cache hits or redirects)

- For ordering constraints, build a dependency graph from the Before/After declarations and topologically sort — this is simpler than priority numbers and less error-prone

- The streaming ResponseTransformer can use `io.Pipe` to connect the buffered writer to the transformer — write to the pipe in one goroutine, read transformed output in another

## Success Criteria

1. A chain with Authentication → RateLimiter → Handler correctly short-circuits on rate limit (returns 429) while still logging the request via RequestLogger's PostProcessor

2. A chain where a handler panics routes the panic to PanicRecovery, which returns a 500 response, and PostProcessors still execute

3. Chain construction fails with a clear error when a middleware consuming user identity is placed before the Authentication middleware that provides it

4. ResponseCompressor correctly gzip-compresses response bodies and sets the Content-Encoding header, but only when the client sends Accept-Encoding: gzip

5. The BufferedResponseWriter correctly delegates Flusher and Hijacker only when the underlying writer supports them — `http.NewResponseController` compatibility

6. Benchmarks show the overhead of interface dispatch and buffering compared to closure-based middleware, with analysis of when the tradeoff is worth it

7. Streaming mode correctly transforms large responses without buffering the entire body in memory — verified by transforming a response larger than available buffer size

8. Ordering constraints correctly place middleware: declaring `RequestLogger.Before("Authentication")` ensures logging wraps authentication, even if registered in the opposite order

## Research Resources

- [Go net/http Handler interface](https://pkg.go.dev/net/http#Handler) — the foundation all middleware builds on

- [Go http.ResponseController](https://pkg.go.dev/net/http#ResponseController) — Go 1.20+ response writer feature detection

- [Alice middleware chaining](https://github.com/justinas/alice) — a popular closure-based middleware chain for comparison

- [chi middleware](https://github.com/go-chi/chi/tree/master/middleware) — production middleware implementations for reference

- [Decorator Pattern](https://refactoring.guru/design-patterns/decorator) — the underlying design pattern
