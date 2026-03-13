# 6. Custom Slog Handler

<!--
difficulty: advanced
concepts: [slog-handler-interface, custom-handler, handler-middleware, colored-output, log-routing]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [slog-basics, json-handler-vs-text-handler, groups-and-nested-attributes, slog-with-logger-enrichment]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 through 05 in this section
- Understanding of interfaces and io.Writer

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the `slog.Handler` interface from scratch
- **Build** a handler that produces colored, human-friendly console output
- **Chain** handlers using a middleware/wrapper pattern

## Why Custom Handlers

The built-in `TextHandler` and `JSONHandler` cover most cases, but sometimes you need output that neither provides: colored terminal output, structured output for a proprietary log format, or a handler that routes different levels to different destinations. The `slog.Handler` interface has only four methods. Implementing it gives you full control over log formatting and routing.

## The Problem

Build a custom slog handler that produces colorized, human-friendly console output. Then build a handler wrapper that fans out log records to multiple handlers.

### Requirements

1. Implement `slog.Handler` with `Enabled`, `Handle`, `WithAttrs`, and `WithGroup`
2. Color-code output by level (Debug=gray, Info=green, Warn=yellow, Error=red)
3. Format attributes as `key=value` pairs after the message
4. Handle groups correctly (prefix attribute keys with group name)
5. Build a `MultiHandler` that dispatches to multiple handlers

### Hints

<details>
<summary>Hint 1: The Handler interface</summary>

```go
type Handler interface {
    Enabled(context.Context, Level) bool
    Handle(context.Context, Record) error
    WithAttrs(attrs []Attr) Handler
    WithGroup(name string) Handler
}
```

`WithAttrs` and `WithGroup` return a **new** handler -- they must not mutate the original.
</details>

<details>
<summary>Hint 2: Immutable state for WithAttrs and WithGroup</summary>

Store pre-computed attributes and group prefixes in the handler struct. Each call to `WithAttrs` or `WithGroup` creates a new handler with a copy of the existing state plus the new data:

```go
type PrettyHandler struct {
    opts   PrettyHandlerOptions
    attrs  []slog.Attr
    groups []string
    mu     *sync.Mutex
    w      io.Writer
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    newAttrs := make([]slog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
    copy(newAttrs, h.attrs)
    newAttrs = append(newAttrs, attrs...)
    return &PrettyHandler{
        opts:   h.opts,
        attrs:  newAttrs,
        groups: h.groups,
        mu:     h.mu,
        w:      h.w,
    }
}
```
</details>

<details>
<summary>Hint 3: Color codes</summary>

ANSI escape codes for terminal colors:

```go
const (
    colorReset  = "\033[0m"
    colorGray   = "\033[90m"
    colorGreen  = "\033[32m"
    colorYellow = "\033[33m"
    colorRed    = "\033[31m"
)
```
</details>

<details>
<summary>Hint 4: MultiHandler pattern</summary>

```go
type MultiHandler struct {
    handlers []slog.Handler
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
    for _, h := range m.handlers {
        if err := h.Handle(ctx, r.Clone()); err != nil {
            return err
        }
    }
    return nil
}
```

Use `r.Clone()` because handlers may consume the record's attributes iterator.
</details>

## Verification

Your program should produce output similar to:

```
10:30:00 DBG detailed trace component=auth
10:30:00 INF application started version=1.2.3
10:30:00 WRN cache miss rate high rate=0.35
10:30:00 ERR connection failed host=db.example.com error=timeout
10:30:00 INF request completed request.method=GET request.path=/api/users request.status=200
```

With terminal colors: DBG in gray, INF in green, WRN in yellow, ERR in red.

The `MultiHandler` should simultaneously write colored output to stderr and JSON to a file:

```bash
go run main.go
cat app.log  # JSON output
```

## What's Next

Continue to [07 - Context-Aware Logging](../07-context-aware-logging/07-context-aware-logging.md) to integrate slog with `context.Context` for trace correlation.

## Summary

- The `slog.Handler` interface has four methods: `Enabled`, `Handle`, `WithAttrs`, `WithGroup`
- `WithAttrs` and `WithGroup` must return new handlers -- never mutate the receiver
- Use `record.Attrs(func(slog.Attr) bool)` to iterate over a record's attributes
- A `MultiHandler` fans out records to multiple handlers using `record.Clone()`
- Custom handlers enable colored output, routing, filtering, and proprietary formats

## Reference

- [slog.Handler interface](https://pkg.go.dev/log/slog#Handler)
- [slog handler guide](https://github.com/golang/example/blob/master/slog-handler-guide/README.md)
- [slog.Record](https://pkg.go.dev/log/slog#Record)
