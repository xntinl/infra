# 3. JSON Handler vs Text Handler

<!--
difficulty: intermediate
concepts: [json-handler, text-handler, handler-options, source-location, output-format]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [slog-basics, log-levels-and-filtering]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Log Levels and Filtering](../02-log-levels-and-filtering/02-log-levels-and-filtering.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Compare** `slog.TextHandler` and `slog.JSONHandler` output formats
- **Configure** handler options including source location and time formatting
- **Choose** the appropriate handler based on the deployment environment

## Why JSON Handler vs Text Handler

Text output is human-friendly during local development. JSON output is machine-friendly in production, where log aggregators like Elasticsearch, Datadog, and CloudWatch parse structured fields automatically. Choosing the right handler for the right environment avoids the pain of parsing free-text logs in production or reading JSON walls during development.

## The Problem

Build a program that logs the same events through both handlers and compare the output. Then configure a handler that switches format based on an environment variable.

## Requirements

1. Log identical events through `TextHandler` and `JSONHandler`
2. Enable source location to include file and line number
3. Write a factory function that returns the appropriate handler based on an environment variable
4. Use `slog.SetDefault` to set the global logger

## Step 1 -- Compare Output Formats

```bash
mkdir -p ~/go-exercises/slog-handlers
cd ~/go-exercises/slog-handlers
go mod init slog-handlers
```

Create `main.go`:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	textHandler := slog.NewTextHandler(os.Stdout, nil)
	textLogger := slog.New(textHandler)

	jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
	jsonLogger := slog.New(jsonHandler)

	textLogger.Info("user action", "user", "alice", "action", "login", "ip", "192.168.1.1")
	jsonLogger.Info("user action", "user", "alice", "action", "login", "ip", "192.168.1.1")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
time=2025-01-15T10:30:00.000Z level=INFO msg="user action" user=alice action=login ip=192.168.1.1
{"time":"2025-01-15T10:30:00.000Z","level":"INFO","msg":"user action","user":"alice","action":"login","ip":"192.168.1.1"}
```

## Step 2 -- Enable Source Location

Add `AddSource: true` to see where each log call originates:

```go
package main

import (
	"log/slog"
	"os"
)

func main() {
	opts := &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelDebug,
	}

	textLogger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	jsonLogger := slog.New(slog.NewJSONHandler(os.Stdout, opts))

	textLogger.Info("with source", "key", "value")
	jsonLogger.Info("with source", "key", "value")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (paths will differ):

```
time=2025-01-15T10:30:00.000Z level=INFO source=/home/user/main.go:16 msg="with source" key=value
{"time":"2025-01-15T10:30:00.000Z","level":"INFO","source":{"function":"main.main","file":"/home/user/main.go","line":17},"msg":"with source","key":"value"}
```

Notice how JSON embeds source as a nested object with function, file, and line.

## Step 3 -- Environment-Based Handler Selection

Create a factory function that selects the handler based on an environment variable:

```go
package main

import (
	"log/slog"
	"os"
)

func newLogger(env string) *slog.Logger {
	opts := &slog.HandlerOptions{
		AddSource: env != "production",
	}

	switch env {
	case "production":
		opts.Level = slog.LevelInfo
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	default:
		opts.Level = slog.LevelDebug
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
}

func main() {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	logger := newLogger(env)
	slog.SetDefault(logger)

	slog.Info("application started", "env", env)
	slog.Debug("debug info", "config_loaded", true)
	slog.Warn("deprecation notice", "feature", "v1-api", "sunset", "2025-06-01")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (development mode, text output with debug visible):

```
time=2025-01-15T10:30:00.000Z level=INFO source=main.go:28 msg="application started" env=development
time=2025-01-15T10:30:00.000Z level=DEBUG source=main.go:29 msg="debug info" config_loaded=true
time=2025-01-15T10:30:00.000Z level=WARN source=main.go:30 msg="deprecation notice" feature=v1-api sunset=2025-06-01
```

Now try production mode:

```bash
APP_ENV=production go run main.go
```

Expected (JSON, no debug, no source):

```
{"time":"2025-01-15T10:30:00.000Z","level":"INFO","msg":"application started","env":"production"}
{"time":"2025-01-15T10:30:00.000Z","level":"WARN","msg":"deprecation notice","feature":"v1-api","sunset":"2025-06-01"}
```

## Step 4 -- Writing to a File

Handlers accept any `io.Writer`. Log to both stderr and a file:

```go
package main

import (
	"io"
	"log/slog"
	"os"
)

func main() {
	f, err := os.Create("app.log")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// Write JSON to the file, text to stderr
	multi := io.MultiWriter(os.Stderr, f)
	logger := slog.New(slog.NewJSONHandler(multi, nil))
	slog.SetDefault(logger)

	slog.Info("logged to both destinations", "file", "app.log")
}
```

### Intermediate Verification

```bash
go run main.go
cat app.log
```

Both stderr and `app.log` contain the JSON log line.

## Verification

Your final program should demonstrate:

1. Text handler output for development
2. JSON handler output for production
3. Environment-based handler selection
4. Source location when enabled

```bash
go run main.go
APP_ENV=production go run main.go
```

## What's Next

Continue to [04 - Groups and Nested Attributes](../04-groups-and-nested-attributes/04-groups-and-nested-attributes.md) to organize log attributes into logical groups.

## Summary

- `slog.TextHandler` produces `key=value` output for human consumption
- `slog.JSONHandler` produces JSON for machine consumption
- `AddSource: true` includes file, line, and function in log records
- Both handlers accept any `io.Writer` -- stderr, stdout, files, or multi-writers
- Use environment variables to select the handler at startup
- JSON in production, text in development is a common pattern

## Reference

- [slog.TextHandler](https://pkg.go.dev/log/slog#TextHandler)
- [slog.JSONHandler](https://pkg.go.dev/log/slog#JSONHandler)
- [slog.HandlerOptions](https://pkg.go.dev/log/slog#HandlerOptions)
