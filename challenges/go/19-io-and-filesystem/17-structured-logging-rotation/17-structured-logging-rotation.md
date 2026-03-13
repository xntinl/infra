# Exercise 17: Structured Logging with Rotation

**Difficulty:** Advanced | **Estimated Time:** 35 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Production logging requires more than `fmt.Println`. You need structured fields for machine parsing, log levels for filtering, file rotation to manage disk space, and concurrent safety. This exercise combines Go's `log/slog` package (Go 1.21+) with custom `io.Writer` implementations to build a production-grade logging system.

## Prerequisites

- File I/O (Exercise 01)
- `io.Writer` interface (Exercise 02)
- `sync.Mutex` for concurrency
- `time` package

## Problem

### Part 1: slog Basics

Use Go's built-in `log/slog` package to set up structured logging:

```go
import "log/slog"

logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

logger.Info("request processed",
    "method", "GET",
    "path", "/api/users",
    "status", 200,
    "duration_ms", 42,
)
```

Create loggers with both `JSONHandler` (for machine consumption) and `TextHandler` (for humans). Log messages at all levels (Debug, Info, Warn, Error) with structured fields.

### Part 2: Rotating File Writer

Implement a `RotatingWriter` that automatically rotates log files based on size:

```go
type RotatingWriter struct {
    mu          sync.Mutex
    dir         string
    prefix      string
    maxSize     int64
    file        *os.File
    currentSize int64
}
```

Behavior:
- Write to `{dir}/{prefix}-{timestamp}.log`
- When `currentSize` exceeds `maxSize`, close the current file and open a new one with a new timestamp
- Thread-safe (protected by mutex)
- Implements `io.Writer` so it can be passed to `slog.NewJSONHandler`

### Part 3: Log File Cleanup

Add a retention policy to `RotatingWriter`:

```go
func (rw *RotatingWriter) Cleanup(maxAge time.Duration, maxFiles int) error
```

- Delete log files older than `maxAge`
- Keep at most `maxFiles` files (delete oldest first)
- Run this on a ticker in a background goroutine

### Part 4: Multi-Output Logger

Build a logger that writes to multiple destinations simultaneously:

1. JSON to a rotating file (for log aggregation)
2. Colored text to stderr (for developer console)
3. Error-level-and-above to a separate error log file

Use `io.MultiWriter` for the combined output, or create a custom `slog.Handler` that fans out to multiple handlers.

### Part 5: Context-Enriched Logging

Create a logging middleware pattern:

```go
func WithRequestID(logger *slog.Logger, requestID string) *slog.Logger {
    return logger.With("request_id", requestID)
}
```

Demonstrate passing enriched loggers through a call chain, where each layer adds its own fields. Show the final log output with all accumulated fields.

## Hints

- `slog.NewJSONHandler(w, opts)` accepts any `io.Writer` -- this is where your `RotatingWriter` plugs in.
- For rotation, use `time.Now().Format("2006-01-02T15-04-05")` in the filename (colons are invalid in filenames on some OSes).
- The mutex must protect both the write and the size check + rotation. Do not let two goroutines race on rotation.
- For cleanup, `filepath.Glob(filepath.Join(dir, prefix+"-*.log"))` finds all log files. Sort by name (timestamps sort lexicographically).
- `slog.Handler` interface: implement `Enabled`, `Handle`, `WithAttrs`, `WithGroup` for a custom multi-handler.
- Alternatively, use `io.MultiWriter` for a simpler approach (but you lose per-destination level filtering).
- For colored text output, ANSI escape codes: `\033[31m` (red), `\033[33m` (yellow), `\033[0m` (reset).

## Verification Criteria

- Part 1: JSON and text log output with structured fields at all levels
- Part 2: log files rotate when size limit is reached; new file has a new timestamp; old file is intact
- Part 3: old files are deleted by the cleanup routine; file count stays within limits
- Part 4: the same log message appears in multiple destinations in the appropriate format
- Part 5: nested With() calls produce log entries with accumulated fields
- Thread safety: run 100 goroutines logging simultaneously -- no panics or corrupted output

## Stretch Goals

- Add gzip compression for rotated files (compress old logs in the background)
- Implement `io.Closer` on the `RotatingWriter` to flush and close the current file
- Add metrics: count log messages per level, track rotation events
- Implement a ring buffer writer that keeps the last N log entries in memory for crash dumps
- Add sampling: only log 1 in N debug messages to reduce volume

## Key Takeaways

- `log/slog` is Go's standard structured logging package (Go 1.21+)
- `JSONHandler` for machines, `TextHandler` for humans -- both accept any `io.Writer`
- Log rotation is implemented as a custom `io.Writer` that manages file lifecycle
- Mutex protection is essential since log writers are called from multiple goroutines
- `slog.Logger.With()` creates child loggers with additional fields -- perfect for request-scoped context
- Separate error logs help with alerting without parsing the full log stream
