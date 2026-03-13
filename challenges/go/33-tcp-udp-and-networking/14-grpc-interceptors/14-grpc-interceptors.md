# 14. gRPC Interceptors

<!--
difficulty: advanced
concepts: [grpc-interceptor, unary-interceptor, stream-interceptor, metadata, grpc-middleware, chain]
tools: [go, protoc]
estimated_time: 40m
bloom_level: apply
prerequisites: [grpc-streaming, interfaces, context]
-->

## Prerequisites

- Go 1.22+ installed
- Completed gRPC Streaming exercise
- Understanding of Go interfaces and `context.Context`
- Familiarity with middleware patterns (HTTP middleware, decorators)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** unary and stream interceptors for both gRPC server and client
- **Design** interceptor chains for logging, authentication, metrics, and rate limiting
- **Analyze** metadata propagation through gRPC interceptors using `metadata.MD`
- **Apply** interceptor composition to add cross-cutting concerns without modifying service logic

## Why gRPC Interceptors Matter

Interceptors are gRPC's equivalent of HTTP middleware. They wrap every RPC call, letting you add logging, authentication, metrics collection, rate limiting, and error handling in a single place. Without interceptors, you would duplicate this logic in every RPC method. Server interceptors run before the handler; client interceptors run before the request is sent. Understanding interceptors is essential for building production gRPC services.

## The Problem

Build a set of gRPC interceptors that add cross-cutting concerns to any gRPC service:

1. Logging interceptor that records method name, duration, and status
2. Auth interceptor that validates a bearer token from metadata
3. Metrics interceptor that tracks request counts and latencies per method
4. Recovery interceptor that catches panics in handlers and returns Internal errors

## Requirements

1. **Unary server interceptor** -- implement logging that records method, duration, and gRPC status code for every unary call
2. **Stream server interceptor** -- implement logging for stream RPCs that records method, duration, and message count
3. **Auth interceptor** -- extract a bearer token from `metadata.MD`, validate it, and reject unauthorized requests with `codes.Unauthenticated`
4. **Unary client interceptor** -- automatically inject an authorization token into outgoing metadata
5. **Metrics interceptor** -- track per-method call count, error count, and latency histogram using atomic counters
6. **Recovery interceptor** -- catch panics in handlers, log the stack trace, and return `codes.Internal`
7. **Interceptor chaining** -- compose multiple interceptors using `grpc.ChainUnaryInterceptor` and `grpc.ChainStreamInterceptor`
8. **Tests** -- verify interceptor execution order, auth rejection, panic recovery, and metrics accuracy

## Hints

<details>
<summary>Hint 1: Unary server interceptor signature</summary>

```go
func loggingInterceptor(
    ctx context.Context,
    req any,
    info *grpc.UnaryServerInfo,
    handler grpc.UnaryHandler,
) (any, error) {
    start := time.Now()
    resp, err := handler(ctx, req)
    st, _ := status.FromError(err)
    log.Printf("method=%s duration=%s status=%s", info.FullMethod, time.Since(start), st.Code())
    return resp, err
}
```

</details>

<details>
<summary>Hint 2: Auth from metadata</summary>

```go
func authInterceptor(
    ctx context.Context,
    req any,
    info *grpc.UnaryServerInfo,
    handler grpc.UnaryHandler,
) (any, error) {
    md, ok := metadata.FromIncomingContext(ctx)
    if !ok {
        return nil, status.Error(codes.Unauthenticated, "missing metadata")
    }
    tokens := md.Get("authorization")
    if len(tokens) == 0 || tokens[0] != "Bearer valid-token" {
        return nil, status.Error(codes.Unauthenticated, "invalid token")
    }
    return handler(ctx, req)
}
```

</details>

<details>
<summary>Hint 3: Chaining interceptors</summary>

```go
srv := grpc.NewServer(
    grpc.ChainUnaryInterceptor(
        recoveryInterceptor,
        loggingInterceptor,
        authInterceptor,
        metricsInterceptor,
    ),
    grpc.ChainStreamInterceptor(
        streamLoggingInterceptor,
        streamAuthInterceptor,
    ),
)
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Logging interceptor records method name and duration for every call
- Auth interceptor rejects requests without a valid token with `codes.Unauthenticated`
- Auth interceptor passes requests with a valid token
- Recovery interceptor catches panics and returns `codes.Internal` instead of crashing
- Metrics interceptor accurately counts requests and errors per method
- Interceptors execute in the order they are chained

## What's Next

Continue to [15 - Custom HTTP Transport](../15-custom-http-transport/15-custom-http-transport.md) to build custom HTTP transports with connection control and request transformation.

## Summary

- gRPC interceptors wrap RPC calls like HTTP middleware wraps handlers
- Unary interceptors use `grpc.UnaryServerInterceptor` / `grpc.UnaryClientInterceptor`
- Stream interceptors use `grpc.StreamServerInterceptor` / `grpc.StreamClientInterceptor`
- Metadata (`metadata.MD`) carries request-scoped data like auth tokens through interceptors
- `grpc.ChainUnaryInterceptor` composes multiple interceptors in order
- Recovery interceptors prevent panics from crashing the entire gRPC server
- Client interceptors can inject metadata (auth tokens, trace IDs) into every outgoing request

## Reference

- [gRPC Go Interceptors](https://grpc.io/docs/languages/go/interceptors/)
- [grpc.UnaryServerInterceptor](https://pkg.go.dev/google.golang.org/grpc#UnaryServerInterceptor)
- [grpc metadata package](https://pkg.go.dev/google.golang.org/grpc/metadata)
- [grpc-ecosystem/go-grpc-middleware](https://github.com/grpc-ecosystem/go-grpc-middleware)
