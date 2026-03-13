# 5. Slog with Logger Enrichment

<!--
difficulty: intermediate
concepts: [logger-with, contextual-logging, logger-enrichment, slog-logvaluer, request-scoped-logging]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [slog-basics, groups-and-nested-attributes]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Groups and Nested Attributes](../04-groups-and-nested-attributes/04-groups-and-nested-attributes.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `logger.With` to create enriched loggers with persistent attributes
- **Implement** the `slog.LogValuer` interface for custom types
- **Build** request-scoped loggers that carry context through a call chain

## Why Logger Enrichment

When handling an HTTP request, you want every log line to include the request ID, user ID, and trace ID without passing those values to every log call. `logger.With` creates a child logger that automatically includes those attributes. This eliminates boilerplate and ensures consistent, correlated log output.

`slog.LogValuer` lets your types control their own log representation. A `User` struct might log only its ID and role, omitting sensitive fields like email or password hash.

## The Problem

Build a request-handling pipeline where each layer adds its own context to the logger. Implement `LogValuer` on a domain type so it logs a safe representation.

## Requirements

1. Use `logger.With` to attach request-scoped attributes
2. Pass enriched loggers through a call chain
3. Implement `slog.LogValuer` on a custom type
4. Demonstrate that child loggers inherit parent attributes

## Step 1 -- Basic Logger Enrichment with With

```bash
mkdir -p ~/go-exercises/slog-enrichment
cd ~/go-exercises/slog-enrichment
go mod init slog-enrichment
```

Create `main.go`:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	base := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Create an enriched logger with service-level attributes
	logger := base.With(
		slog.String("service", "order-api"),
		slog.String("version", "1.4.2"),
	)

	logger.Info("application started")
	logger.Info("listening", "port", 8080)
	logger.Warn("cache miss rate high", "rate", 0.35)
}
```

### Intermediate Verification

```bash
go run main.go
```

Every log line includes `service` and `version` automatically.

## Step 2 -- Request-Scoped Logger

Build a child logger for each request:

```go
package main

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
)

func generateID() string {
	return fmt.Sprintf("%08x", rand.Int31())
}

func handleOrder(logger *slog.Logger, orderID string) {
	logger = logger.With(slog.String("order_id", orderID))

	logger.Info("processing order")
	chargePayment(logger, orderID, 99.95)
	logger.Info("order completed")
}

func chargePayment(logger *slog.Logger, orderID string, amount float64) {
	logger.Info("charging payment",
		slog.Float64("amount", amount),
		slog.String("currency", "USD"),
	)
}

func main() {
	base := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger := base.With(slog.String("service", "order-api"))

	// Simulate handling two requests
	for i := 0; i < 2; i++ {
		reqLogger := logger.With(slog.String("request_id", generateID()))
		handleOrder(reqLogger, fmt.Sprintf("ORD-%03d", i+1))
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Each log line includes `service`, `request_id`, and `order_id`. Lines from the same request share the same `request_id`.

## Step 3 -- LogValuer Interface

Implement `slog.LogValuer` so a type controls its log representation:

```go
package main

import (
	"log/slog"
	"os"
)

type User struct {
	ID           string
	Name         string
	Email        string
	Role         string
	PasswordHash string
}

// LogValue controls what gets logged -- omit sensitive fields
func (u User) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", u.ID),
		slog.String("name", u.Name),
		slog.String("role", u.Role),
	)
}

type CreditCard struct {
	Number string
	Expiry string
}

func (c CreditCard) LogValue() slog.Value {
	masked := "****-****-****-" + c.Number[len(c.Number)-4:]
	return slog.GroupValue(
		slog.String("number", masked),
		slog.String("expiry", c.Expiry),
	)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	user := User{
		ID:           "usr-42",
		Name:         "Alice",
		Email:        "alice@example.com",
		Role:         "admin",
		PasswordHash: "$2a$10$abcdef...",
	}

	card := CreditCard{
		Number: "4111111111111234",
		Expiry: "12/26",
	}

	logger.Info("payment processed",
		slog.Any("user", user),
		slog.Any("card", card),
		slog.Float64("amount", 49.99),
	)
}
```

### Intermediate Verification

```bash
go run main.go | python3 -m json.tool
```

Expected: `user` contains only `id`, `name`, `role` -- no email or password hash. `card.number` is masked.

## Step 4 -- Combining Enrichment Patterns

Bring all patterns together:

```go
package main

import (
	"log/slog"
	"os"
)

type Service struct {
	Name    string
	Version string
	logger  *slog.Logger
}

func NewService(name, version string) *Service {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		slog.String("service", name),
		slog.String("version", version),
	)
	return &Service{Name: name, Version: version, logger: logger}
}

func (s *Service) HandleRequest(requestID string) {
	logger := s.logger.With(slog.String("request_id", requestID))
	logger.Info("request received")

	// Pass the enriched logger to sub-operations
	s.processItem(logger, "item-001")
	s.processItem(logger, "item-002")

	logger.Info("request completed")
}

func (s *Service) processItem(logger *slog.Logger, itemID string) {
	logger = logger.With(slog.String("item_id", itemID))
	logger.Info("processing item")
}

func main() {
	svc := NewService("inventory-api", "2.1.0")
	svc.HandleRequest("req-abc")
	svc.HandleRequest("req-def")
}
```

### Intermediate Verification

```bash
go run main.go
```

Each log line accumulates attributes: `service`, `version`, `request_id`, and where applicable, `item_id`.

## Verification

Run the program and confirm:

1. `With` adds persistent attributes to all subsequent log calls
2. Child loggers inherit parent attributes
3. `LogValuer` controls which fields appear in logs
4. Sensitive data is excluded from log output

```bash
go run main.go
```

## What's Next

Continue to [06 - Custom Slog Handler](../06-custom-slog-handler/06-custom-slog-handler.md) to build your own handler from scratch.

## Summary

- `logger.With(attrs...)` returns a new logger with persistent attributes
- Child loggers inherit all parent attributes -- attributes accumulate through the call chain
- `slog.LogValuer` interface lets types control their log representation
- Use `LogValuer` to redact sensitive fields (passwords, credit cards, tokens)
- Pass enriched loggers as function parameters to propagate context
- Each `With` call is cheap -- it does not copy the handler

## Reference

- [slog.Logger.With](https://pkg.go.dev/log/slog#Logger.With)
- [slog.LogValuer](https://pkg.go.dev/log/slog#LogValuer)
- [Structured Logging with slog](https://go.dev/blog/slog)
