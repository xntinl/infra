# 4. Groups and Nested Attributes

<!--
difficulty: intermediate
concepts: [slog-group, nested-json, attribute-grouping, withgroup, slog-attr]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [slog-basics, json-handler-vs-text-handler]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - JSON Handler vs Text Handler](../03-json-handler-vs-text-handler/03-json-handler-vs-text-handler.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `slog.Group` to create nested attribute structures
- **Apply** `logger.WithGroup` to prefix all subsequent attributes
- **Design** log schemas that match your JSON query patterns

## Why Groups and Nested Attributes

Flat key-value pairs become ambiguous when you log related data. If you log `id=123` and `id=456`, which is the user ID and which is the order ID? Groups namespace attributes: `user.id=123 order.id=456`. In JSON output, groups produce nested objects that log aggregators can index and query independently.

## The Problem

Build a request logger that organizes attributes into logical groups: request details, user details, and response details. The JSON output should produce nested objects that a log aggregator can query with dot notation.

## Requirements

1. Use `slog.Group` to create inline nested attributes
2. Use `logger.WithGroup` to add a persistent group prefix
3. Produce JSON output with nested objects
4. Demonstrate how groups appear differently in text vs JSON handlers

## Step 1 -- Inline Groups with slog.Group

```bash
mkdir -p ~/go-exercises/slog-groups
cd ~/go-exercises/slog-groups
go mod init slog-groups
```

Create `main.go`:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	logger.Info("request completed",
		slog.Group("request",
			slog.String("method", "GET"),
			slog.String("path", "/api/users/42"),
			slog.String("ip", "192.168.1.1"),
		),
		slog.Group("response",
			slog.Int("status", 200),
			slog.Int("bytes", 1024),
		),
	)
}
```

### Intermediate Verification

```bash
go run main.go | python3 -m json.tool
```

Expected (formatted):

```json
{
    "time": "2025-01-15T10:30:00.000Z",
    "level": "INFO",
    "msg": "request completed",
    "request": {
        "method": "GET",
        "path": "/api/users/42",
        "ip": "192.168.1.1"
    },
    "response": {
        "status": 200,
        "bytes": 1024
    }
}
```

Now try the same with a text handler to see the difference:

```go
textLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
textLogger.Info("request completed",
    slog.Group("request",
        slog.String("method", "GET"),
        slog.String("path", "/api/users/42"),
    ),
    slog.Group("response",
        slog.Int("status", 200),
    ),
)
```

Text output uses dot notation:

```
time=2025-01-15T10:30:00.000Z level=INFO msg="request completed" request.method=GET request.path=/api/users/42 response.status=200
```

## Step 2 -- Persistent Groups with WithGroup

`WithGroup` adds a group prefix to all attributes logged by the returned logger:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	base := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Create a logger scoped to the "auth" group
	authLogger := base.WithGroup("auth")

	authLogger.Info("login attempt",
		slog.String("user", "alice"),
		slog.String("method", "oauth2"),
		slog.Bool("success", true),
	)

	authLogger.Warn("suspicious activity",
		slog.String("user", "bob"),
		slog.Int("failed_attempts", 5),
	)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (each line is a JSON object):

```json
{"time":"...","level":"INFO","msg":"login attempt","auth":{"user":"alice","method":"oauth2","success":true}}
{"time":"...","level":"WARN","msg":"suspicious activity","auth":{"user":"bob","failed_attempts":5}}
```

Every attribute logged through `authLogger` is nested under `"auth"`.

## Step 3 -- Nested Groups

Groups can be nested for deeper structure:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	logger.Info("order placed",
		slog.Group("order",
			slog.String("id", "ORD-001"),
			slog.Float64("total", 99.95),
			slog.Group("customer",
				slog.String("id", "USR-42"),
				slog.String("name", "Alice"),
			),
			slog.Group("shipping",
				slog.String("method", "express"),
				slog.String("country", "US"),
			),
		),
	)
}
```

### Intermediate Verification

```bash
go run main.go | python3 -m json.tool
```

Expected:

```json
{
    "time": "...",
    "level": "INFO",
    "msg": "order placed",
    "order": {
        "id": "ORD-001",
        "total": 99.95,
        "customer": {
            "id": "USR-42",
            "name": "Alice"
        },
        "shipping": {
            "method": "express",
            "country": "US"
        }
    }
}
```

## Step 4 -- Combining WithGroup and With

Chain `WithGroup` and `With` to build contextual loggers:

```go
package main

import (
	"log/slog"
	"os"
)

type RequestContext struct {
	TraceID   string
	UserID    string
	SessionID string
}

func handleRequest(logger *slog.Logger, reqCtx RequestContext) {
	// Add request context as a group
	reqLogger := logger.WithGroup("request").With(
		slog.String("trace_id", reqCtx.TraceID),
		slog.String("user_id", reqCtx.UserID),
	)

	reqLogger.Info("processing started")

	// Further scope to a specific operation
	dbLogger := reqLogger.WithGroup("db")
	dbLogger.Info("query executed",
		slog.String("table", "orders"),
		slog.Int("rows", 15),
	)

	reqLogger.Info("processing completed", slog.Int("status", 200))
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	handleRequest(logger, RequestContext{
		TraceID:   "abc-123",
		UserID:    "usr-42",
		SessionID: "sess-789",
	})
}
```

### Intermediate Verification

```bash
go run main.go
```

The db query log will have nested structure: `request.trace_id`, `request.user_id`, and `request.db.table`, `request.db.rows`.

## Verification

Run the program and inspect the JSON output. Confirm:

1. `slog.Group` creates nested JSON objects
2. `WithGroup` prefixes all subsequent attributes
3. Groups nest within groups
4. Text handler shows dot notation instead of nesting

```bash
go run main.go | python3 -m json.tool
```

## What's Next

Continue to [05 - Slog with Logger Enrichment](../05-slog-with-for-logger-enrichment/05-slog-with-logger-enrichment.md) to learn how to attach persistent attributes to loggers.

## Summary

- `slog.Group("name", attrs...)` creates inline nested attributes
- In JSON output, groups become nested objects; in text, they use dot notation
- `logger.WithGroup("name")` returns a logger that nests all future attributes under that group
- Groups can be nested for deep hierarchical structure
- Combine `WithGroup` and `With` to build contextual loggers for request handling
- Design groups to match your log aggregator's query patterns

## Reference

- [slog.Group](https://pkg.go.dev/log/slog#Group)
- [slog.Logger.WithGroup](https://pkg.go.dev/log/slog#Logger.WithGroup)
- [Structured Logging with slog](https://go.dev/blog/slog)
