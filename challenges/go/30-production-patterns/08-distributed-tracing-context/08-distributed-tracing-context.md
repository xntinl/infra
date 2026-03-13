<!--
difficulty: advanced
concepts: distributed-tracing, context-propagation, w3c-tracecontext, span-links, baggage
tools: go.opentelemetry.io/otel, go.opentelemetry.io/otel/propagation, net/http
estimated_time: 40m
bloom_level: applying
prerequisites: opentelemetry-basics, context, http-middleware, request-id-propagation
-->

# Exercise 30.8: Distributed Tracing Context

## Prerequisites

Before starting this exercise, you should be comfortable with:

- OpenTelemetry basics (Exercise 30.7)
- Context propagation
- HTTP middleware and client patterns
- Request ID propagation (Exercise 30.5)

## Learning Objectives

By the end of this exercise, you will be able to:

1. Propagate trace context across service boundaries using W3C TraceContext headers
2. Create linked spans for asynchronous workflows (e.g., message queues)
3. Use OpenTelemetry baggage to pass business context across services
4. Build an instrumented HTTP client that automatically creates child spans

## Why This Matters

A single user action in a microservices architecture can touch 5, 10, or 50 services. Without distributed tracing, debugging latency issues or failures means manually correlating logs across services. Trace context propagation ensures that every span across every service shares a single trace ID, giving you an end-to-end view of the request in tools like Jaeger or Tempo.

---

## Problem

Build a three-service system (API Gateway, Order Service, Payment Service) where trace context propagates automatically across HTTP boundaries. Demonstrate parent-child span relationships, baggage propagation, and span links for async operations.

### Hints

- `otelhttp.NewTransport` wraps `http.RoundTripper` to inject trace headers on outgoing requests
- `otelhttp.NewHandler` extracts trace headers from incoming requests and creates server spans
- W3C TraceContext uses `traceparent` and `tracestate` headers
- Baggage is propagated via the `baggage` header and accessed with `baggage.FromContext`
- Span links connect causally related but not parent-child spans (e.g., a message producer and consumer)

### Step 1: Create the project

```bash
mkdir -p distributed-tracing && cd distributed-tracing
go mod init distributed-tracing
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/sdk
go get go.opentelemetry.io/otel/exporters/stdout/stdouttrace
go get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
go get go.opentelemetry.io/otel/baggage
```

### Step 2: Create a shared telemetry setup

Create `telemetry.go`:

```go
package main

import (
	"context"
	"io"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func initTracer(ctx context.Context, serviceName string, w io.Writer) (*sdktrace.TracerProvider, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithWriter(w), stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	res, _ := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(serviceName),
	))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter), // synchronous for demo clarity
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}
```

### Step 3: Build the three services

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
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	ctx := context.Background()

	tp, err := initTracer(ctx, "api-gateway", os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	defer tp.Shutdown(ctx)

	// Instrumented HTTP client that propagates trace context
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   10 * time.Second,
	}

	// --- Payment Service (port 8082) ---
	paymentMux := http.NewServeMux()
	paymentMux.HandleFunc("POST /charge", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		span := trace.SpanFromContext(ctx)

		// Read baggage
		bag := baggage.FromContext(ctx)
		customerID := bag.Member("customer.id").Value()
		span.SetAttributes(attribute.String("customer.id", customerID))

		amount := float64(rand.Intn(10000)) / 100.0
		time.Sleep(time.Duration(30+rand.Intn(70)) * time.Millisecond)

		span.SetAttributes(attribute.Float64("payment.amount", amount))
		span.AddEvent("payment.charged", trace.WithAttributes(
			attribute.Float64("amount", amount),
			attribute.String("currency", "USD"),
		))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"charged":     true,
			"amount":      amount,
			"customer_id": customerID,
		})
	})

	go func() {
		log.Println("Payment Service on :8082")
		http.ListenAndServe(":8082", otelhttp.NewHandler(paymentMux, "payment-service"))
	}()

	// --- Order Service (port 8081) ---
	orderMux := http.NewServeMux()
	orderMux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tracer := otel.Tracer("order-service")

		// Create a child span for order processing
		ctx, processSpan := tracer.Start(ctx, "process-order")
		orderID := fmt.Sprintf("ORD-%06d", rand.Intn(999999))
		processSpan.SetAttributes(attribute.String("order.id", orderID))
		time.Sleep(20 * time.Millisecond) // simulate DB write
		processSpan.End()

		// Call Payment Service
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost:8082/charge", nil)
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "payment failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var paymentResult map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&paymentResult)

		// Simulate async event (span link, not parent-child)
		parentSpan := trace.SpanFromContext(ctx)
		_, asyncSpan := tracer.Start(context.Background(), "emit-order-event",
			trace.WithLinks(trace.Link{
				SpanContext: parentSpan.SpanContext(),
				Attributes: []attribute.KeyValue{
					attribute.String("link.reason", "async-event-trigger"),
				},
			}),
		)
		asyncSpan.SetAttributes(attribute.String("event.type", "order.created"))
		time.Sleep(5 * time.Millisecond)
		asyncSpan.End()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"order_id": orderID,
			"payment":  paymentResult,
		})
	})

	go func() {
		log.Println("Order Service on :8081")
		http.ListenAndServe(":8081", otelhttp.NewHandler(orderMux, "order-service"))
	}()

	// --- API Gateway (port 8080) ---
	gatewayMux := http.NewServeMux()
	gatewayMux.HandleFunc("POST /api/orders", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		customerID := r.URL.Query().Get("customer_id")
		if customerID == "" {
			customerID = "anonymous"
		}

		// Set baggage for downstream services
		member, _ := baggage.NewMember("customer.id", customerID)
		bag, _ := baggage.New(member)
		ctx = baggage.ContextWithBaggage(ctx, bag)

		// Call Order Service
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost:8081/orders", nil)
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "order service error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	log.Println("API Gateway on :8080")
	log.Fatal(http.ListenAndServe(":8080", otelhttp.NewHandler(gatewayMux, "api-gateway")))
}
```

### Step 4: Test the trace propagation

```bash
go run . &
sleep 1

curl -s -X POST "localhost:8080/api/orders?customer_id=cust-42" | jq .

kill %1
```

In the stdout trace output, you should see:
- A root span from the API Gateway
- A child span for the Order Service HTTP request
- A grandchild span for the Payment Service HTTP request
- A linked (not child) span for the async event emission
- The `customer.id` baggage value appearing in the Payment Service span

---

## Verify

```bash
go build -o server . && ./server > /tmp/trace-output.txt 2>&1 &
sleep 1
curl -s -X POST "localhost:8080/api/orders?customer_id=verify-test" > /dev/null
sleep 1
kill %1
grep -c "TraceID" /tmp/trace-output.txt
```

There should be multiple spans sharing the same TraceID, confirming context propagated across all three services.

---

## What's Next

In the next exercise, you will implement a circuit breaker with half-open state management to protect your service from cascading failures.

## Summary

- W3C TraceContext (`traceparent`/`tracestate` headers) is the standard for distributed trace propagation
- `otelhttp.NewTransport` injects trace headers on outgoing HTTP requests automatically
- `otelhttp.NewHandler` extracts trace headers from incoming requests
- Baggage carries business context (customer ID, tenant ID) across service boundaries
- Span links connect related but non-parent-child spans (e.g., async event flows)

## Reference

- [W3C Trace Context specification](https://www.w3.org/TR/trace-context/)
- [OpenTelemetry context propagation](https://opentelemetry.io/docs/concepts/context-propagation/)
- [otelhttp instrumentation](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp)
- [Baggage specification](https://opentelemetry.io/docs/concepts/signals/baggage/)
