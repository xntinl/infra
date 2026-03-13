# 6. Goroutine Dump Analysis

<!--
difficulty: advanced
concepts: [goroutine-dump, sigquit, runtime-stack, pprof-goroutine, debug-pprof]
tools: [go, curl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, http-programming, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines and synchronization primitives
- Familiarity with HTTP servers and signal handling
- Basic knowledge of stack traces

## Learning Objectives

After completing this exercise, you will be able to:

- **Capture** goroutine dumps using SIGQUIT, `runtime.Stack`, and `net/http/pprof`
- **Analyze** goroutine stack traces to identify blocked, running, and idle goroutines
- **Diagnose** production issues by reading goroutine states and wait reasons
- **Implement** a debug endpoint that exposes goroutine dumps on demand

## Why Goroutine Dump Analysis Matters

When a Go application hangs or becomes unresponsive in production, a goroutine dump is your primary diagnostic tool. It shows every goroutine, its state (running, waiting, blocked), and its full stack trace. This lets you identify deadlocks, goroutine leaks, resource exhaustion, and blocking operations.

Unlike core dumps that require specialized tools, goroutine dumps are human-readable text. Learning to read them quickly is a critical production debugging skill.

## The Problem

Build an HTTP server with a debug endpoint that exposes goroutine dumps. Then create several pathological conditions (deadlock, leak, blocking I/O) and practice reading the dumps to diagnose them.

## Requirements

1. **SIGQUIT dump** -- send SIGQUIT to a Go process and capture the full goroutine dump from stderr
2. **runtime.Stack** -- programmatically capture all goroutine stacks using `runtime.Stack(buf, true)`
3. **pprof endpoint** -- expose goroutine dumps via `net/http/pprof` at `/debug/pprof/goroutine?debug=2`
4. **Custom debug endpoint** -- implement `/debug/goroutines` that returns a formatted goroutine dump with counts by state
5. **Analysis exercises** -- create programs with known issues and practice reading the dumps to identify:
   - Which goroutines are blocked and on what
   - Where goroutine leaks are accumulating
   - What the wait reasons mean (`chan receive`, `semacquire`, `IO wait`)
6. **Tests** -- verify the debug endpoint returns goroutine information

## Hints

<details>
<summary>Hint 1: Capturing stacks programmatically</summary>

```go
func captureStacks() string {
    buf := make([]byte, 1024*1024) // 1MB buffer
    n := runtime.Stack(buf, true)  // true = all goroutines
    return string(buf[:n])
}
```

</details>

<details>
<summary>Hint 2: Goroutine state summary</summary>

```go
import "runtime/pprof"

func goroutineSummary(w io.Writer) {
    profile := pprof.Lookup("goroutine")
    profile.WriteTo(w, 1) // debug level 1 = summary
    // debug level 2 = full stacks
}
```

</details>

<details>
<summary>Hint 3: Reading goroutine states</summary>

Common goroutine states in dumps:
- `running` -- actively executing
- `runnable` -- ready to run, waiting for a CPU
- `chan receive` -- blocked on channel read
- `chan send` -- blocked on channel write
- `semacquire` -- blocked on mutex or WaitGroup
- `IO wait` -- blocked on network or file I/O
- `select` -- blocked in a select statement
- `sleep` -- in time.Sleep

</details>

<details>
<summary>Hint 4: Registering pprof</summary>

```go
import _ "net/http/pprof"

// pprof registers its handlers on the default mux.
// If using a custom mux:
mux.HandleFunc("/debug/pprof/", pprof.Index)
mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
```

</details>

## Verification

```bash
go test -v -race ./...

# Manual testing
go run main.go &
PID=$!

# pprof endpoint
curl http://localhost:8080/debug/pprof/goroutine?debug=2

# Custom endpoint
curl http://localhost:8080/debug/goroutines

# SIGQUIT (prints dump to stderr and exits)
kill -QUIT $PID
```

Your output should show:
- Full goroutine stacks with state information
- The custom endpoint provides a count of goroutines by state
- You can identify blocked goroutines and their wait reasons
- The debug endpoint is accessible without authentication (in development only)

## What's Next

Continue to [07 - Concurrent Test Isolation](../07-concurrent-test-isolation/07-concurrent-test-isolation.md) to learn patterns for isolating concurrent tests from each other.

## Summary

- SIGQUIT triggers a goroutine dump to stderr and terminates the process
- `runtime.Stack(buf, true)` captures all goroutine stacks programmatically
- `net/http/pprof` exposes goroutine dumps at `/debug/pprof/goroutine?debug=2`
- Goroutine states (`chan receive`, `semacquire`, `IO wait`) tell you what each goroutine is waiting for
- Reading goroutine dumps is the primary tool for diagnosing hangs and leaks in production
- Never expose pprof endpoints without authentication in production

## Reference

- [runtime.Stack](https://pkg.go.dev/runtime#Stack)
- [net/http/pprof](https://pkg.go.dev/net/http/pprof)
- [Diagnostics](https://go.dev/doc/diagnostics)
