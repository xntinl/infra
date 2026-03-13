# 8. Chaos Testing Concurrent Code

<!--
difficulty: advanced
concepts: [chaos-testing, fault-injection, goroutine-scheduling, stress-testing, jitter, failure-modes]
tools: [go]
estimated_time: 40m
bloom_level: evaluate
prerequisites: [testing-concurrent-code, context, sync-primitives, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Testing Concurrent Code exercise
- Strong understanding of goroutines, channels, context, and error handling
- Familiarity with fault injection concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** chaos tests that inject failures into concurrent Go systems
- **Evaluate** system resilience under random delays, cancellations, and panics
- **Implement** fault injection hooks for testing error paths in concurrent code
- **Measure** system behavior under degraded conditions (slow dependencies, partial failures)

## Why Chaos Testing Concurrent Code Matters

Unit tests verify the happy path. Race detector catches data races. But neither tests what happens when a goroutine takes 100x longer than expected, when a context is cancelled mid-operation, when a dependency randomly fails, or when the system is under extreme load.

Chaos testing for concurrent code injects these failures deliberately during tests to verify that your synchronization, error handling, and cleanup logic hold up under adversity. This is where you find goroutine leaks, resource exhaustion, and cascading failures before production does.

## The Problem

You have a pipeline system that processes items through three stages (fetch, transform, store), each running as concurrent goroutines connected by channels. Build a chaos test framework that:

1. Injects random delays to change goroutine scheduling
2. Randomly cancels contexts to test shutdown paths
3. Injects errors into pipeline stages to test error propagation
4. Applies back-pressure by slowing consumers

## Requirements

1. **Pipeline implementation** -- a three-stage concurrent pipeline (fetch -> transform -> store) using channels
2. **Fault injector** -- a configurable injector that can: add random delays (0-100ms), return errors with configurable probability (0-100%), cancel context after N operations, panic with configurable probability
3. **Chaos-wrapped stages** -- wrap each pipeline stage with the fault injector
4. **Invariant checks** -- after chaos runs, verify: no goroutine leaks (use goleak), no data loss (all items either processed or errored), no data duplication, resources cleaned up
5. **Stress test** -- run the pipeline with 1000 items, chaos enabled, 10 concurrent runs
6. **Metrics** -- track processed, errored, and dropped items; verify they sum to the total input

## Hints

<details>
<summary>Hint 1: Fault injector</summary>

```go
type FaultInjector struct {
    ErrorRate    float64       // 0.0 to 1.0
    MaxDelay     time.Duration
    PanicRate    float64
    rng          *rand.Rand
}

func (f *FaultInjector) MaybeInject() error {
    if f.MaxDelay > 0 {
        delay := time.Duration(f.rng.Int63n(int64(f.MaxDelay)))
        time.Sleep(delay)
    }
    if f.rng.Float64() < f.PanicRate {
        panic("injected panic")
    }
    if f.rng.Float64() < f.ErrorRate {
        return errors.New("injected error")
    }
    return nil
}
```

</details>

<details>
<summary>Hint 2: Panic-safe goroutine wrapper</summary>

```go
func safeGo(f func()) <-chan error {
    errCh := make(chan error, 1)
    go func() {
        defer func() {
            if r := recover(); r != nil {
                errCh <- fmt.Errorf("panic: %v", r)
            }
            close(errCh)
        }()
        f()
    }()
    return errCh
}
```

</details>

<details>
<summary>Hint 3: Pipeline with chaos</summary>

```go
func (p *Pipeline) Run(ctx context.Context, items []Item) (Results, error) {
    fetchOut := make(chan Item, 10)
    transformOut := make(chan Item, 10)

    g, ctx := errgroup.WithContext(ctx)

    g.Go(func() error {
        defer close(fetchOut)
        for _, item := range items {
            if err := p.chaos.MaybeInject(); err != nil {
                p.results.AddError(item, err)
                continue
            }
            select {
            case fetchOut <- p.fetch(item):
            case <-ctx.Done():
                return ctx.Err()
            }
        }
        return nil
    })
    // ... transform and store stages
}
```

</details>

## Verification

```bash
go test -v -race -count=5 -timeout 60s ./...
```

Your tests should:
- Run the pipeline with chaos enabled and verify invariants hold
- Confirm no goroutine leaks after each chaos run
- Show that processed + errored items equals total input (no data loss or duplication)
- Demonstrate the pipeline handles context cancellation cleanly
- Recover from panics in individual stages without crashing the pipeline
- Pass consistently despite random fault injection

## What's Next

You have completed the Concurrency Debugging and Testing section. Continue to [Section 33 - TCP/UDP and Networking](../../33-tcp-udp-and-networking/01-tcp-server-and-client/01-tcp-server-and-client.md).

## Summary

- Chaos testing injects failures (delays, errors, cancellations, panics) into concurrent code during tests
- A fault injector should be configurable: error rate, delay range, panic rate
- After chaos runs, verify invariants: no leaks, no data loss, no duplication, proper cleanup
- Wrap goroutines with panic recovery to prevent cascading crashes
- Use `errgroup` for coordinated goroutine lifecycle with error propagation
- Run chaos tests with `-race -count=N` to test under different scheduling conditions

## Reference

- [errgroup package](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Chaos engineering principles](https://principlesofchaos.org/)
- [Testing in Go](https://go.dev/doc/tutorial/add-a-test)
