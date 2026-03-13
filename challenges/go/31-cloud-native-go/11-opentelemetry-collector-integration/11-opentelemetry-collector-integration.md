# 11. OpenTelemetry Collector Integration

<!--
difficulty: advanced
concepts: [opentelemetry, otel-sdk, trace-exporter, span, context-propagation, otlp, collector]
tools: [go, otel-collector, docker]
estimated_time: 40m
bloom_level: analyze
prerequisites: [http-programming, context, prometheus-metrics-exposition]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of HTTP middleware and context propagation
- Completed Prometheus Metrics Exposition exercise
- Familiarity with distributed tracing concepts (spans, traces, context)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** OpenTelemetry tracing in a Go application using the OTel SDK
- **Analyze** trace context propagation across HTTP boundaries
- **Configure** an OTLP exporter to send telemetry to an OpenTelemetry Collector
- **Design** meaningful spans with attributes, events, and status codes

## Why OpenTelemetry Integration Matters

OpenTelemetry is the vendor-neutral standard for observability. Unlike Prometheus (pull-based metrics), OTel provides traces, metrics, and logs through a push-based model. The OTel Collector acts as a pipeline that receives, processes, and exports telemetry to backends like Jaeger, Zipkin, Datadog, or Prometheus.

Integrating the OTel SDK into your Go application gives you distributed tracing (follow a request across services), automatic HTTP instrumentation, and the ability to add custom spans for business logic visibility. The Collector decouples your application from the backend, letting you change observability vendors without code changes.

## The Problem

Build an HTTP API server with OpenTelemetry tracing that:

1. Creates spans for each incoming HTTP request
2. Adds custom spans for business logic (database queries, external API calls)
3. Propagates trace context to downstream HTTP calls
4. Exports traces via OTLP to the OpenTelemetry Collector
5. Records span attributes and events for debugging

## Requirements

1. **Tracer provider** -- configure an OTLP gRPC exporter and batch span processor
2. **HTTP middleware** -- use `otelhttp` to automatically instrument incoming requests
3. **Custom spans** -- create child spans for simulated database queries and external API calls
4. **Span attributes** -- add `user.id`, `order.id`, `db.statement` attributes to relevant spans
5. **Span events** -- record events for cache hits/misses within spans
6. **Error recording** -- set span status to Error and record error details on failures
7. **Context propagation** -- propagate trace context on outgoing HTTP requests using `otelhttp.NewTransport`
8. **Tests** -- use an in-memory exporter to capture and assert spans in tests

## Hints

<details>
<summary>Hint 1: Tracer provider setup</summary>

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func initTracer(ctx context.Context) (*trace.TracerProvider, error) {
    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint("localhost:4317"),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String("my-api"),
        )),
    )
    otel.SetTracerProvider(tp)
    return tp, nil
}
```

</details>

<details>
<summary>Hint 2: Custom spans</summary>

```go
tracer := otel.Tracer("my-api")

func getOrder(ctx context.Context, orderID string) (Order, error) {
    ctx, span := tracer.Start(ctx, "getOrder")
    defer span.End()

    span.SetAttributes(attribute.String("order.id", orderID))

    order, err := queryDB(ctx, orderID)
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return Order{}, err
    }

    span.AddEvent("order_loaded", trace.WithAttributes(
        attribute.Int("item_count", len(order.Items)),
    ))
    return order, nil
}
```

</details>

<details>
<summary>Hint 3: In-memory exporter for tests</summary>

```go
import "go.opentelemetry.io/otel/sdk/trace/tracetest"

exporter := tracetest.NewInMemoryExporter()
tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
otel.SetTracerProvider(tp)

// ... exercise the code ...

spans := exporter.GetSpans()
// assert span names, attributes, status
```

</details>

## Verification

```bash
go test -v -race ./...

# With a running collector (optional)
docker run -p 4317:4317 otel/opentelemetry-collector:latest &
go run main.go &
curl http://localhost:8080/api/orders/123
```

Your tests should confirm:
- HTTP requests create spans with correct method and path attributes
- Custom spans appear as children of the HTTP span
- Span attributes like `order.id` are recorded
- Errors set span status to Error and record the error
- Span events are captured (cache hit/miss)
- Context propagation creates parent-child span relationships

## What's Next

You have completed the Cloud-Native Go section. Continue to [Section 32 - Concurrency Debugging and Testing](../../32-concurrency-debugging-and-testing/01-race-condition-reproduction/01-race-condition-reproduction.md).

## Summary

- OpenTelemetry SDK provides vendor-neutral tracing, metrics, and logs for Go applications
- Use `otelhttp` middleware for automatic HTTP instrumentation and context propagation
- Create custom spans with `tracer.Start(ctx, name)` for business logic visibility
- Add attributes, events, and error recording to spans for debugging
- Export via OTLP to the OpenTelemetry Collector, which routes telemetry to any backend
- Test with `tracetest.NewInMemoryExporter()` for deterministic span assertions

## Reference

- [OpenTelemetry Go documentation](https://opentelemetry.io/docs/languages/go/)
- [OTel Go SDK](https://pkg.go.dev/go.opentelemetry.io/otel)
- [OTLP exporter](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
