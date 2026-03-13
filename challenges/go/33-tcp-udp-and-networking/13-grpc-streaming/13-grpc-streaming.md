# 13. gRPC Streaming

<!--
difficulty: advanced
concepts: [grpc, protobuf, server-streaming, client-streaming, bidirectional-streaming, grpc-server]
tools: [go, protoc, protoc-gen-go, protoc-gen-go-grpc]
estimated_time: 40m
bloom_level: apply
prerequisites: [tcp-server-and-client, concurrent-tcp-server, interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- `protoc` compiler and Go gRPC plugins installed (`protoc-gen-go`, `protoc-gen-go-grpc`)
- Understanding of TCP connections and Go interfaces
- Basic familiarity with Protocol Buffers syntax

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** all four gRPC communication patterns: unary, server streaming, client streaming, and bidirectional streaming
- **Design** Protocol Buffer service definitions with streaming RPCs
- **Analyze** streaming flow control and backpressure behavior in gRPC
- **Integrate** gRPC server and client with proper error handling and context cancellation

## Why gRPC Streaming Matters

REST APIs use a request-response model where the client sends one request and receives one response. gRPC extends this with streaming, allowing either side (or both) to send a sequence of messages over a single RPC call. This is essential for real-time data feeds, file uploads in chunks, chat systems, and any scenario where data arrives incrementally.

gRPC runs over HTTP/2, which provides multiplexing, flow control, and header compression. Understanding streaming patterns lets you build efficient, real-time services that would be awkward or impossible with REST.

## The Problem

Build a metrics aggregation service with four RPC methods demonstrating each gRPC pattern:

1. **Unary** -- get a single metric summary by name
2. **Server streaming** -- subscribe to a metric and receive periodic updates
3. **Client streaming** -- upload a batch of metric data points and receive a summary
4. **Bidirectional streaming** -- send data points and receive running aggregations in real time

## Requirements

1. **Proto definition** -- define a `MetricsService` with all four RPC patterns
2. **Server streaming** -- the server sends a metric update every 500ms for a configurable duration, the client prints each update as it arrives
3. **Client streaming** -- the client sends a sequence of `DataPoint` messages, the server computes count, sum, mean, min, and max, returning a `Summary` when the client closes the stream
4. **Bidirectional streaming** -- the client sends data points and the server sends back a running aggregate after each received point
5. **Context cancellation** -- demonstrate cancelling a server stream from the client side using `context.WithTimeout`
6. **Error handling** -- use gRPC status codes (`codes.InvalidArgument`, `codes.NotFound`) for error responses
7. **Tests** -- test all four patterns using an in-process gRPC server (no network)

## Hints

<details>
<summary>Hint 1: Proto service definition</summary>

```protobuf
service MetricsService {
  rpc GetSummary(MetricRequest) returns (Summary);
  rpc Subscribe(SubscribeRequest) returns (stream MetricUpdate);
  rpc Upload(stream DataPoint) returns (Summary);
  rpc Monitor(stream DataPoint) returns (stream Summary);
}
```

</details>

<details>
<summary>Hint 2: Server streaming implementation</summary>

```go
func (s *server) Subscribe(req *pb.SubscribeRequest, stream pb.MetricsService_SubscribeServer) error {
    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    for i := 0; i < int(req.Count); i++ {
        select {
        case <-stream.Context().Done():
            return stream.Context().Err()
        case <-ticker.C:
            if err := stream.Send(&pb.MetricUpdate{
                Name:  req.Name,
                Value: computeValue(),
            }); err != nil {
                return err
            }
        }
    }
    return nil
}
```

</details>

<details>
<summary>Hint 3: In-process test server</summary>

```go
func setupTestServer(t *testing.T) pb.MetricsServiceClient {
    lis := bufconn.Listen(1024 * 1024)
    srv := grpc.NewServer()
    pb.RegisterMetricsServiceServer(srv, &server{})

    go srv.Serve(lis)
    t.Cleanup(srv.GracefulStop)

    conn, _ := grpc.NewClient("passthrough:///bufconn",
        grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
            return lis.DialContext(ctx)
        }),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    t.Cleanup(func() { conn.Close() })
    return pb.NewMetricsServiceClient(conn)
}
```

</details>

## Verification

```bash
protoc --go_out=. --go-grpc_out=. proto/metrics.proto
go test -v -race ./...
```

Your tests should confirm:
- Unary RPC returns a valid summary for a known metric
- Server stream delivers the expected number of updates
- Client stream computes correct aggregate statistics
- Bidirectional stream returns updated aggregates after each data point
- Cancelling a stream context stops the server from sending further updates

## What's Next

Continue to [14 - gRPC Interceptors](../14-grpc-interceptors/14-grpc-interceptors.md) to add cross-cutting concerns like logging, authentication, and metrics to gRPC services.

## Summary

- gRPC supports four patterns: unary, server streaming, client streaming, and bidirectional streaming
- Streaming RPCs keep a single HTTP/2 connection open and send multiple messages in either direction
- Server streams use `stream.Send()`; client streams use `stream.Recv()` in a loop until `io.EOF`
- Bidirectional streams combine both: each side sends and receives independently
- Always check `stream.Context().Done()` for cancellation in long-running streams
- Use `bufconn` for in-process testing without network overhead

## Reference

- [gRPC Go Quick Start](https://grpc.io/docs/languages/go/quickstart/)
- [gRPC Concepts -- RPCs](https://grpc.io/docs/what-is-grpc/core-concepts/#rpc-life-cycle)
- [google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc)
- [grpc/grpc-go bufconn](https://pkg.go.dev/google.golang.org/grpc/test/bufconn)
