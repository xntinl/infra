# 9. Replacing Global Logger Patterns

<!--
difficulty: advanced
concepts: [slog-setdefault, log-slog-bridge, global-logger, dependency-injection-logger, testing-logs]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [slog-basics, json-handler-vs-text-handler, slog-with-logger-enrichment]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 through 05 in this section
- Familiarity with the standard `log` package

## Learning Objectives

After completing this exercise, you will be able to:

- **Replace** the global slog logger with `slog.SetDefault`
- **Bridge** legacy `log.Println` calls to structured slog output
- **Compare** global logger vs dependency injection approaches
- **Capture** log output in tests

## Why Replacing Global Logger Patterns

Most Go projects start with `log.Println` scattered throughout the codebase. Migrating to structured logging requires a strategy. `slog.SetDefault` lets you redirect both `slog` and the old `log` package to a structured handler. This means you can migrate incrementally -- new code uses `slog.Info`, old code continues with `log.Println`, and both produce structured output.

For testing, you need to capture log output to assert that critical events are logged. Global loggers make this difficult; dependency injection makes it straightforward.

## The Problem

Build a program that demonstrates three patterns for managing loggers: the global default, the `log`-to-`slog` bridge, and dependency injection. Then write tests that capture and verify log output.

### Requirements

1. Configure `slog.SetDefault` with a JSON handler
2. Show that `log.Println` output flows through the slog handler
3. Refactor to accept `*slog.Logger` as a dependency
4. Write a test that captures log output into a buffer
5. Compare the trade-offs of global vs injected loggers

### Hints

<details>
<summary>Hint 1: The log-to-slog bridge</summary>

When you call `slog.SetDefault(logger)`, it also sets the standard `log` package's output to go through the slog handler:

```go
slog.SetDefault(logger)
// Now log.Println("hello") produces structured output
log.Println("this goes through slog")
```

The message appears at Info level with the `msg` key.
</details>

<details>
<summary>Hint 2: Capturing log output in tests</summary>

```go
func TestLogging(t *testing.T) {
    var buf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&buf, nil))

    // Pass logger to your code
    myFunction(logger)

    // Assert on captured output
    output := buf.String()
    if !strings.Contains(output, `"msg":"expected message"`) {
        t.Errorf("expected log message not found in: %s", output)
    }
}
```
</details>

<details>
<summary>Hint 3: Dependency injection pattern</summary>

```go
type OrderService struct {
    logger *slog.Logger
    // ... other dependencies
}

func NewOrderService(logger *slog.Logger) *OrderService {
    return &OrderService{
        logger: logger.With(slog.String("component", "order-service")),
    }
}
```

Each component receives its logger through the constructor and enriches it with component-specific attributes.
</details>

## Verification

Your program should demonstrate:

1. `slog.Info` and `log.Println` both produce JSON output after `SetDefault`
2. A service that accepts `*slog.Logger` via constructor
3. Test output showing captured and verified log lines

```bash
go run main.go
go test -v ./...
```

Expected behavior:
- All log output is JSON
- `log.Println("legacy message")` appears as `{"level":"INFO","msg":"legacy message"}`
- Tests pass, verifying specific log messages were emitted

## What's Next

You have completed all exercises in the structured logging section. Consider revisiting [06 - Custom Slog Handler](../06-custom-slog-handler/06-custom-slog-handler.md) to build more advanced handler patterns, or move on to the next section.

## Summary

- `slog.SetDefault(logger)` sets both the slog default and bridges the `log` package
- After `SetDefault`, `log.Println` calls produce structured output through the slog handler
- Dependency injection is preferred for testability -- pass `*slog.Logger` to constructors
- Capture log output in tests by writing to a `bytes.Buffer`
- Global loggers are convenient for small programs; DI scales better for large codebases
- Enrich injected loggers with `logger.With` to add component-level context

## Reference

- [slog.SetDefault](https://pkg.go.dev/log/slog#SetDefault)
- [slog.NewLogLogger](https://pkg.go.dev/log/slog#NewLogLogger)
- [Testing with slog](https://pkg.go.dev/testing/slogtest)
