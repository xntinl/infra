# 2. Goroutine Leak Detection with goleak

<!--
difficulty: advanced
concepts: [goroutine-leak, goleak, test-cleanup, leaked-goroutine, resource-leak]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, channels-basics, testing-ecosystem]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, channels, and context
- Familiarity with Go testing patterns
- `go.uber.org/goleak` module

## Learning Objectives

After completing this exercise, you will be able to:

- **Detect** goroutine leaks in tests using `uber-go/goleak`
- **Analyze** common goroutine leak patterns and their root causes
- **Integrate** goleak into existing test suites with `TestMain` or per-test checks
- **Fix** leaked goroutines by ensuring proper cleanup with context, channels, or `t.Cleanup`

## Why Goroutine Leak Detection Matters

A goroutine leak occurs when a goroutine is started but never terminates. Since goroutines are cheap to create, leaks often go unnoticed until the application runs out of memory or file descriptors in production. Common causes include blocking channel reads with no sender, forgetting to cancel a context, or starting a background worker without a shutdown mechanism.

`goleak` verifies that no unexpected goroutines are running at the end of a test. This catches leaks during development before they reach production.

## The Problem

You will write code containing several common goroutine leak patterns, detect them with goleak, and fix each one.

## Requirements

1. **Channel leak** -- goroutine blocks on a channel that nobody will ever send to; detect with goleak; fix by using a done channel or context
2. **Ticker leak** -- goroutine runs a `time.NewTicker` but the ticker is never stopped; detect and fix
3. **HTTP server leak** -- an HTTP server is started in a test but never shut down; detect and fix with `t.Cleanup`
4. **Context leak** -- a `context.WithCancel` is created but `cancel()` is never called; detect and fix
5. **Per-test detection** -- use `goleak.VerifyNone(t)` at the end of each test function
6. **TestMain integration** -- show how to use `goleak.VerifyTestMain(m)` for project-wide leak detection

## Hints

<details>
<summary>Hint 1: Basic goleak usage</summary>

```go
import "go.uber.org/goleak"

func TestNoLeak(t *testing.T) {
    defer goleak.VerifyNone(t)

    ch := make(chan int, 1)
    go func() {
        ch <- 42
    }()
    <-ch
}
```

</details>

<details>
<summary>Hint 2: Channel leak pattern</summary>

```go
func leakyFunction() int {
    ch := make(chan int)
    go func() {
        result := expensiveComputation()
        ch <- result // blocks forever if nobody reads from ch
    }()
    // caller returns early without reading from ch
    return 0
}
```

Fix: use a buffered channel `make(chan int, 1)` or read from `ch`.

</details>

<details>
<summary>Hint 3: Ignoring known goroutines</summary>

```go
defer goleak.VerifyNone(t,
    goleak.IgnoreTopFunction("net/http.(*Server).Serve"),
    goleak.IgnoreCurrent(),
)
```

Use this sparingly -- only for goroutines you truly cannot control (e.g., from third-party libraries).

</details>

<details>
<summary>Hint 4: TestMain pattern</summary>

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

This checks for leaked goroutines after all tests in the package complete.

</details>

## Verification

```bash
go get go.uber.org/goleak
go test -v -race ./...
```

Your tests should:
- Fail when a leaky function is tested without fixing the leak
- Pass after applying the fix for each leak pattern
- Demonstrate both per-test (`VerifyNone`) and package-level (`VerifyTestMain`) detection
- Show the goleak error message that identifies the leaked goroutine's stack trace

## What's Next

Continue to [03 - Testing Concurrent Code](../03-testing-concurrent-code/03-testing-concurrent-code.md) to learn deterministic testing patterns for concurrent Go code.

## Summary

- Goroutine leaks occur when goroutines block forever or run indefinitely without a shutdown mechanism
- `goleak.VerifyNone(t)` detects leaked goroutines at the end of each test
- `goleak.VerifyTestMain(m)` checks for leaks after all tests in a package
- Common leak patterns: blocked channel reads, unfinished tickers, uncancelled contexts, unshut HTTP servers
- Fix leaks with buffered channels, `defer cancel()`, `t.Cleanup`, and `defer ticker.Stop()`

## Reference

- [uber-go/goleak](https://pkg.go.dev/go.uber.org/goleak)
- [Goroutine leaks in Go](https://go.dev/blog/context)
- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
