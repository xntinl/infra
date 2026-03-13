# 1. Slog Basics

<!--
difficulty: basic
concepts: [slog, structured-logging, key-value-pairs, log-output, default-logger]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [functions, fmt-package]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with `fmt.Println` and basic Go programs

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** why structured logging exists and how it differs from `fmt.Println` or `log.Println`
- **Use** `slog.Info`, `slog.Warn`, `slog.Error`, and `slog.Debug` to emit structured log messages
- **Add** key-value attributes to log messages

## Why Structured Logging with slog

Traditional logging with `fmt.Println` or `log.Println` produces unstructured text. A line like `user logged in: alice` is easy to read but impossible to parse reliably. If you need to search logs for all logins by a specific user, you need regex -- fragile, slow, and error-prone.

Structured logging emits key-value pairs alongside the message. A structured log entry like `msg="user logged in" user=alice` is both human-readable and machine-parseable. Log aggregation systems (Grafana Loki, Datadog, CloudWatch Logs Insights) can filter on `user=alice` directly.

Go 1.21 added `log/slog` to the standard library. It provides structured logging without any third-party dependencies.

## Step 1 -- Your First slog Call

Create a project and write a program that uses `slog`:

```bash
mkdir -p ~/go-exercises/slog-basics
cd ~/go-exercises/slog-basics
go mod init slog-basics
```

Create `main.go`:

```go
package main

import "log/slog"

func main() {
	slog.Info("application started")
	slog.Info("user logged in", "user", "alice", "role", "admin")
}
```

The first argument is the message. Subsequent arguments are key-value pairs -- `"user"` is the key, `"alice"` is the value.

### Intermediate Verification

```bash
go run main.go
```

Expected output (timestamps will differ):

```
2025/01/15 10:30:00 INFO application started
2025/01/15 10:30:00 INFO user logged in user=alice role=admin
```

## Step 2 -- All Four Log Levels

`slog` provides four log levels: `Debug`, `Info`, `Warn`, and `Error`. Update `main.go`:

```go
package main

import "log/slog"

func main() {
	slog.Debug("detailed trace info", "component", "auth")
	slog.Info("application started", "version", "1.2.3")
	slog.Warn("disk space low", "available_gb", 2)
	slog.Error("database connection failed", "host", "db.example.com", "err", "timeout")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (note: Debug is not printed by default):

```
2025/01/15 10:30:00 INFO application started version=1.2.3
2025/01/15 10:30:00 WARN disk space low available_gb=2
2025/01/15 10:30:00 ERROR database connection failed host=db.example.com err=timeout
```

The Debug line is missing because the default handler's minimum level is `Info`.

## Step 3 -- Typed Attributes with slog.Attr

Instead of alternating keys and values, you can use `slog.Attr` for type safety:

```go
package main

import "log/slog"

func main() {
	slog.Info("request completed",
		slog.String("method", "GET"),
		slog.String("path", "/api/users"),
		slog.Int("status", 200),
		slog.Duration("latency", 42_000_000), // 42ms
	)
}
```

`slog.String`, `slog.Int`, `slog.Duration`, `slog.Bool`, `slog.Float64`, `slog.Time`, and `slog.Any` create typed attributes. These avoid the risk of mismatched key-value pairs where you accidentally pass two keys in a row.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
2025/01/15 10:30:00 INFO request completed method=GET path=/api/users status=200 latency=42ms
```

## Step 4 -- Mixing Styles

You can mix loose key-value pairs and `slog.Attr` in the same call:

```go
package main

import (
	"log/slog"
	"time"
)

func main() {
	start := time.Now()
	// simulate work
	time.Sleep(10 * time.Millisecond)

	slog.Info("request handled",
		"method", "POST",
		slog.String("path", "/api/orders"),
		slog.Int("status", 201),
		slog.Duration("latency", time.Since(start)),
	)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (latency will vary):

```
2025/01/15 10:30:00 INFO request handled method=POST path=/api/orders status=201 latency=10.123ms
```

## Step 5 -- Logging with Error Values

When logging errors, pass the error as an attribute value:

```go
package main

import (
	"errors"
	"log/slog"
)

func main() {
	err := errors.New("connection refused")

	// Good: error as a named attribute
	slog.Error("failed to connect", "err", err)

	// Also good: using slog.Any
	slog.Error("failed to connect", slog.Any("error", err))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
2025/01/15 10:30:00 ERROR failed to connect err="connection refused"
2025/01/15 10:30:00 ERROR failed to connect error="connection refused"
```

## Common Mistakes

### Odd Number of Key-Value Arguments

**Wrong:**

```go
slog.Info("event", "key1", "val1", "key2") // missing value for key2
```

**What happens:** `slog` emits a malformed log entry with a `!BADKEY` marker.

**Fix:** Always provide values in pairs, or use `slog.Attr` helpers.

### Using fmt.Sprintf in the Message

**Wrong:**

```go
slog.Info(fmt.Sprintf("user %s logged in", username))
```

**What happens:** The structured data is baked into the message string, making it unparseable.

**Fix:** Use attributes: `slog.Info("user logged in", "user", username)`.

### Logging Sensitive Data

**Wrong:**

```go
slog.Info("auth", "password", password)
```

**What happens:** Credentials appear in your logs.

**Fix:** Never log passwords, tokens, or secrets. Log identifiers instead.

## Verify What You Learned

Run the final program and confirm you see structured output with key-value pairs for Info, Warn, and Error levels. Debug output should be absent with the default logger.

```bash
go run main.go
```

## What's Next

Continue to [02 - Log Levels and Filtering](../02-log-levels-and-filtering/02-log-levels-and-filtering.md) to learn how to control which log levels are emitted and enable Debug output.

## Summary

- `log/slog` is Go's standard library for structured logging (Go 1.21+)
- Four levels: `Debug`, `Info`, `Warn`, `Error`
- Key-value pairs follow the message: `slog.Info("msg", "key", value)`
- `slog.String`, `slog.Int`, etc. create typed attributes for safety
- The default handler uses `Info` as the minimum level -- `Debug` is suppressed
- Never embed dynamic data into the message string -- use attributes instead

## Reference

- [log/slog package](https://pkg.go.dev/log/slog)
- [Structured Logging with slog](https://go.dev/blog/slog)
- [slog proposal](https://github.com/golang/go/issues/56345)
