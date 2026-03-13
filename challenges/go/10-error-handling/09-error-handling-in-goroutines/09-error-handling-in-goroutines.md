# 9. Error Handling in Goroutines

<!--
difficulty: advanced
concepts: [goroutine-errors, error-channels, errgroup, concurrent-error-handling]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [error-interface, goroutines, channels, sync-waitgroup]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [08 - Panic vs Error](../08-panic-vs-error/08-panic-vs-error.md)
- Familiarity with goroutines, channels, and `sync.WaitGroup`

## Learning Objectives

After completing this exercise, you will be able to:

- **Propagate** errors from goroutines back to the caller using channels
- **Use** `golang.org/x/sync/errgroup` to manage goroutine lifecycles with error handling
- **Design** concurrent error collection strategies

## Why Error Handling in Goroutines Is Different

A goroutine cannot return a value to its launcher. When a goroutine encounters an error, it cannot simply `return err` -- nobody is waiting for that return value. You need an explicit mechanism to communicate the error back.

There are three common approaches:
1. **Error channels**: send errors through a channel the caller reads.
2. **Shared result struct with mutex**: write errors to shared state (fragile, rarely recommended).
3. **`errgroup.Group`**: a higher-level abstraction from the `x/sync` package that combines `WaitGroup` with error propagation and optional context cancellation.

## The Problem

Build a concurrent URL health checker that fetches multiple URLs in parallel and collects errors from any that fail. Implement two versions:

1. A channel-based version where goroutines send errors through a channel.
2. An `errgroup`-based version that cancels remaining work when the first error occurs.

### Requirements

- Define a `checkURL(url string) error` function that simulates HTTP checks (use a map of URLs to success/failure rather than real HTTP calls).
- **Channel version**: launch one goroutine per URL, collect all errors, report them at the end.
- **Errgroup version**: use `errgroup.WithContext` so that when one URL fails, the context is cancelled and remaining goroutines can bail out early.
- Print which URLs succeeded and which failed.

### Hints

<details>
<summary>Hint 1: Channel-based approach</summary>

Create a buffered channel with capacity equal to the number of URLs. Each goroutine sends either `nil` or an error. The main goroutine reads exactly N results.

```go
errs := make(chan error, len(urls))
for _, u := range urls {
    go func(url string) {
        errs <- checkURL(url)
    }(u)
}
```
</details>

<details>
<summary>Hint 2: Collecting results from a channel</summary>

```go
var failures []error
for range urls {
    if err := <-errs; err != nil {
        failures = append(failures, err)
    }
}
```
</details>

<details>
<summary>Hint 3: errgroup setup</summary>

```go
import "golang.org/x/sync/errgroup"

g, ctx := errgroup.WithContext(context.Background())
for _, u := range urls {
    url := u
    g.Go(func() error {
        // Check ctx.Err() before doing work
        if ctx.Err() != nil {
            return ctx.Err()
        }
        return checkURL(url)
    })
}
if err := g.Wait(); err != nil {
    fmt.Println("First error:", err)
}
```
</details>

<details>
<summary>Hint 4: errgroup collects all vs first</summary>

`errgroup.Wait()` returns only the first non-nil error. If you need all errors, use the channel approach or a custom collector with a mutex.
</details>

## Verification

Your channel-based version should output something like:

```
--- Channel-based error collection ---
Checking 5 URLs concurrently...
Failures:
  https://api.example.com/down: service unavailable
  https://api.example.com/timeout: request timeout
Successes: 3, Failures: 2
```

Your errgroup version should output:

```
--- Errgroup with cancellation ---
First error: service unavailable
Remaining goroutines cancelled via context
```

Run:

```bash
go run main.go
```

## What's Next

Continue to [10 - Error Handling Middleware](../10-error-handling-middleware/10-error-handling-middleware.md) to learn how to centralize error handling in HTTP servers.

## Summary

- Goroutines cannot return errors directly -- use channels or `errgroup`
- Buffered channels with capacity = number of goroutines prevent leaks
- `errgroup.WithContext` cancels remaining work on first error
- `errgroup.Wait` returns only the first error; use channels for collecting all errors
- Always ensure goroutines have a way to report errors -- silent failures are bugs

## Reference

- [errgroup package](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Go blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
