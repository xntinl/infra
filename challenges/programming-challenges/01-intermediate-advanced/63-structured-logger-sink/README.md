# 63. Structured Logger with Sinks

<!--
difficulty: intermediate-advanced
category: observability-and-monitoring
languages: [go, rust]
concepts: [structured-logging, sinks, formatters, log-levels, sampling, file-rotation, async-io]
estimated_time: 4-5 hours
bloom_level: apply, analyze
prerequisites: [go-basics, rust-basics, interfaces, traits, concurrency, file-io, json-serialization]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Go: `log/slog` package concepts (handlers, attributes, levels)
- Rust: `std::io::Write` trait, `serde` for JSON serialization
- Buffered I/O and flushing semantics in both languages
- Channel-based concurrency (Go channels, Rust `mpsc`)
- File system operations: open, write, rename, stat

## Learning Objectives

- **Implement** a structured logging library with pluggable output sinks and formatters
- **Apply** the Strategy pattern to swap between JSON and human-readable text formatters
- **Design** an asynchronous buffered sink that decouples log production from I/O
- **Analyze** the performance impact of synchronous vs. asynchronous logging under high throughput
- **Create** a file rotation mechanism triggered by size or time thresholds

## The Challenge

Logging is the most basic observability signal, yet most applications get it wrong. They use unstructured `fmt.Println` calls that cannot be parsed by log aggregation tools, or they use a logging library but send everything to a single destination. Production systems need structured logs (key-value fields, not interpolated strings), multiple output sinks (stdout for development, files for persistence, network for aggregation), and volume control to prevent high-frequency log paths from overwhelming storage.

Your task is to build a structured logging library from scratch in both Go and Rust. Each log entry has a level, message, timestamp, and arbitrary context fields (key-value pairs). Logs flow through a formatter (JSON or text) and then to one or more sinks (stdout, file, async-buffered). The file sink supports rotation by size or by time. A sampling mechanism allows you to log only 1-in-N entries for high-volume code paths, reducing I/O while preserving statistical visibility.

The Go implementation should align with `log/slog` conventions (Handlers, Attrs, Levels). The Rust implementation should use traits for sinks and formatters, enabling compile-time dispatch.

## Requirements

1. Four log levels: Debug, Info, Warn, Error, with filtering (minimum level per sink)
2. Structured fields: attach key-value pairs per log entry and per logger instance (contextual fields like request ID, service name)
3. Two formatters: JSON (one entry per line, machine-parseable) and Text (human-readable with aligned fields)
4. Stdout sink: writes formatted output to standard output
5. File sink with rotation: rotate when file exceeds a size limit (bytes) or at time intervals; rotated files get a timestamp suffix
6. Async buffered sink: wraps any sink, buffers entries in a channel/queue, flushes on buffer-full, interval, or shutdown
7. Multi-sink fan-out: a logger can write to multiple sinks simultaneously; a slow sink must not block others
8. Sampling: configurable rate (1-in-N) per level; when sampling, a counter field indicates how many entries were suppressed
9. Thread-safe: safe for concurrent use by multiple goroutines/threads
10. Graceful shutdown: flush all buffered entries and close sinks on explicit shutdown call

## Hints

<details>
<summary>Hint 1: Go -- slog-compatible handler</summary>

```go
type MultiHandler struct {
    sinks    []Sink
    minLevel slog.Level
    attrs    []slog.Attr
}

func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
    for _, sink := range h.sinks {
        sink.Write(r)
    }
    return nil
}
```

Implement `slog.Handler` so your logger integrates with the standard library.
</details>

<details>
<summary>Hint 2: Rust -- trait-based sink abstraction</summary>

```rust
pub trait Sink: Send + Sync {
    fn write(&self, entry: &LogEntry) -> Result<(), LogError>;
    fn flush(&self) -> Result<(), LogError>;
}

pub trait Formatter: Send + Sync {
    fn format(&self, entry: &LogEntry) -> String;
}
```

Each sink owns its formatter. `FileSink` holds an `Arc<Mutex<File>>` for thread-safe rotation.
</details>

<details>
<summary>Hint 3: File rotation logic</summary>

Track current file size after each write. When it exceeds the threshold:
1. Close or flush the current file
2. Rename it with a timestamp suffix: `app.log` -> `app.log.2024-01-15T10-30-00`
3. Open a new file with the original name

For time-based rotation, check the elapsed time since last rotation on each write (lazy check, no background goroutine needed).
</details>

<details>
<summary>Hint 4: Sampling with suppression counter</summary>

```go
type Sampler struct {
    rate    int // log 1 in N
    counter atomic.Int64
}

func (s *Sampler) ShouldLog() (bool, int64) {
    n := s.counter.Add(1)
    if n%int64(s.rate) == 0 {
        return true, int64(s.rate) - 1 // suppressed count
    }
    return false, 0
}
```
</details>

## Acceptance Criteria

- [ ] Log entries contain: timestamp, level, message, and arbitrary key-value fields
- [ ] JSON formatter produces valid JSON (one object per line, parseable by `jq`)
- [ ] Text formatter produces aligned, human-readable output
- [ ] File sink rotates when file size exceeds the configured limit
- [ ] Rotated files have timestamp suffixes and the original filename is reused
- [ ] Async sink buffers entries and flushes on interval, buffer-full, or shutdown
- [ ] Multi-sink logger writes to all sinks; a panicking/slow sink does not block others
- [ ] Sampling logs 1-in-N entries and reports suppressed count in the logged entry
- [ ] Concurrent logging from 50+ goroutines/threads produces no races or interleaved output
- [ ] Shutdown flushes all buffered entries before returning
- [ ] Both Go and Rust implementations pass their respective test suites

## Research Resources

- [Go slog package documentation](https://pkg.go.dev/log/slog) -- the standard structured logging API in Go 1.21+
- [Rust tracing crate](https://docs.rs/tracing/latest/tracing/) -- the ecosystem standard for structured logging in Rust
- [12-Factor App: Logs](https://12factor.net/logs) -- treat logs as event streams, not files
- [Uber Zap](https://github.com/uber-go/zap) -- high-performance structured logger for Go, study its buffer and sink design
- [Lumberjack](https://github.com/natefinch/lumberjack) -- Go library for file rotation, reference for rotation logic
- [serde_json](https://docs.rs/serde_json/latest/serde_json/) -- Rust JSON serialization for the JSON formatter
