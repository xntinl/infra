<!--
difficulty: advanced
concepts: opentelemetry, metrics, traces, spans, exporters, instrumentation
tools: go.opentelemetry.io/otel, go.opentelemetry.io/otel/sdk, go.opentelemetry.io/otel/exporters/stdout
estimated_time: 40m
bloom_level: applying
prerequisites: http-server-basics, context, interfaces, structured-logging
-->

# Exercise 30.7: OpenTelemetry Instrumentation

## Prerequisites

Before starting this exercise, you should be comfortable with:

- HTTP server and middleware patterns
- `context.Context` propagation
- Interfaces and dependency injection
- Structured logging

## Learning Objectives

By the end of this exercise, you will be able to:

1. Initialize the OpenTelemetry SDK with trace and metric providers
2. Create spans to trace request flow through your application
3. Record custom metrics (counters, histograms, gauges)
4. Instrument HTTP handlers with automatic span creation and metric recording
5. Export telemetry to stdout (and understand how to swap to OTLP exporters)

## Why This Matters

Observability is not optional in production. OpenTelemetry is the industry standard for collecting traces, metrics, and logs. It provides vendor-neutral instrumentation that works with Jaeger, Prometheus, Datadog, and any OTLP-compatible backend. Understanding OTel basics lets you diagnose latency issues, track error rates, and understand service dependencies.

---

## Problem

Build an HTTP service instrumented with OpenTelemetry that produces traces and metrics. The service should demonstrate manual span creation, automatic HTTP instrumentation, custom metrics, and span attributes.

### Hints

- Initialize a `TracerProvider` with a `SpanExporter` (use `stdouttrace` for this exercise)
- Initialize a `MeterProvider` with a `MetricReader` (use `stdoutmetric`)
- Use `otel.Tracer("service-name")` to get a tracer, then `tracer.Start(ctx, "span-name")` to create spans
- Record span attributes with `span.SetAttributes(attribute.String("key", "value"))`
- For HTTP instrumentation, use `otelhttp.NewHandler` to wrap your mux
- Custom metrics: `meter.Int64Counter`, `meter.Float64Histogram`

### Step 1: Create the project

```bash
mkdir -p otel-basics && cd otel-basics
go mod init otel-basics
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/sdk
go get go.opentelemetry.io/otel/exporters/stdout/stdouttrace
go get go.opentelemetry.io/otel/exporters/stdout/stdoutmetric
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
```

### Step 2: Set up the telemetry provider

Create `telemetry.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func initTelemetry(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// Trace exporter (stdout for demo; swap to OTLP for production)
	traceExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Metric exporter
	metricExporter, err := stdoutmetric.New()
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Propagator for distributed context
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		if err := tp.Shutdown(ctx); err != nil {
			return err
		}
		return mp.Shutdown(ctx)
	}

	return shutdown, nil
}
```

### Step 3: Build the instrumented service

Create `main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	tracer        = otel.Tracer("otel-basics")
	meter         = otel.Meter("otel-basics")
	requestCount  metric.Int64Counter
	requestLatency metric.Float64Histogram
)

func initMetrics() error {
	var err error
	requestCount, err = meter.Int64Counter("http.requests.total",
		metric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		return err
	}
	requestLatency, err = meter.Float64Histogram("http.request.duration_ms",
		metric.WithDescription("HTTP request duration in milliseconds"),
	)
	return err
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	// Create a child span for business logic
	ctx, span := tracer.Start(ctx, "process-order")
	defer span.End()

	orderID := fmt.Sprintf("ORD-%d", rand.Intn(10000))
	span.SetAttributes(
		attribute.String("order.id", orderID),
		attribute.String("order.customer", r.URL.Query().Get("customer")),
	)

	// Simulate database lookup
	if err := simulateDB(ctx, orderID); err != nil {
		span.RecordError(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Simulate payment processing
	amount := simulatePayment(ctx, orderID)

	span.SetAttributes(attribute.Float64("order.amount", amount))

	// Record metrics
	duration := float64(time.Since(start).Milliseconds())
	requestCount.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", r.Method),
		attribute.String("path", "/order"),
		attribute.Int("status", 200),
	))
	requestLatency.Record(ctx, duration, metric.WithAttributes(
		attribute.String("path", "/order"),
	))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"order_id": orderID,
		"amount":   amount,
		"status":   "completed",
	})
}

func simulateDB(ctx context.Context, orderID string) error {
	_, span := tracer.Start(ctx, "db.query")
	defer span.End()
	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.statement", "SELECT * FROM orders WHERE id = ?"),
	)
	time.Sleep(time.Duration(20+rand.Intn(30)) * time.Millisecond)
	return nil
}

func simulatePayment(ctx context.Context, orderID string) float64 {
	_, span := tracer.Start(ctx, "payment.process")
	defer span.End()
	amount := float64(rand.Intn(10000)) / 100.0
	span.SetAttributes(
		attribute.String("payment.provider", "stripe"),
		attribute.Float64("payment.amount", amount),
	)
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	return amount
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	shutdown, err := initTelemetry(ctx, "order-service", "1.0.0")
	if err != nil {
		log.Fatalf("Failed to init telemetry: %v", err)
	}
	defer shutdown(ctx)

	if err := initMetrics(); err != nil {
		log.Fatalf("Failed to init metrics: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /order", handleOrder)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w, "ok")
	})

	handler := otelhttp.NewHandler(mux, "server")

	log.Println("Server listening on :8080")
	server := &http.Server{Addr: ":8080", Handler: handler}

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
```

### Step 4: Test

```bash
go run . &
sleep 2

curl -s localhost:8080/order?customer=alice | jq .
curl -s localhost:8080/order?customer=bob | jq .
curl -s localhost:8080/order?customer=charlie | jq .

# Wait for metric flush
sleep 11

kill %1
```

You should see trace spans printed to stdout showing the parent-child relationship: `server` -> `process-order` -> `db.query` / `payment.process`. After the metric flush interval, you will see counter and histogram data.

---

## Verify

```bash
go build -o server . && ./server > /tmp/otel-output.txt 2>&1 &
sleep 2
curl -s localhost:8080/order?customer=test > /dev/null
sleep 1
kill %1
grep -c "process-order" /tmp/otel-output.txt
```

The grep should find at least 1 match, confirming traces are being exported.

---

## What's Next

In the next exercise, you will build on this foundation to implement distributed tracing context propagation across multiple services.

## Summary

- Initialize `TracerProvider` and `MeterProvider` with appropriate exporters and resources
- Use `otel.Tracer` to create spans that represent units of work
- Add attributes to spans for searchable metadata (IDs, amounts, query strings)
- Use `otelhttp.NewHandler` for automatic HTTP span creation
- Record custom metrics with counters (totals) and histograms (distributions)
- Set up W3C TraceContext propagation for distributed tracing

## Reference

- [OpenTelemetry Go getting started](https://opentelemetry.io/docs/languages/go/getting-started/)
- [go.opentelemetry.io/otel](https://pkg.go.dev/go.opentelemetry.io/otel)
- [Semantic conventions](https://opentelemetry.io/docs/specs/semconv/)
