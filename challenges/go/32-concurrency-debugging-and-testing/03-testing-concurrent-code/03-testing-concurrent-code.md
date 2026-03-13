# 3. Testing Concurrent Code

<!--
difficulty: advanced
concepts: [deterministic-testing, test-synchronization, waitgroup-in-tests, channel-assertions, timeout-patterns]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [testing-ecosystem, goroutines, channels-basics, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of goroutines, channels, and `sync.WaitGroup`
- Familiarity with Go testing patterns (subtests, test helpers)
- Understanding of the race detector

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** deterministic tests for concurrent Go code that do not rely on `time.Sleep`
- **Analyze** flaky test patterns and replace them with synchronization-based assertions
- **Implement** test helpers for common concurrent patterns: timeouts, event ordering, concurrent stress
- **Apply** channel-based assertions to verify goroutine behavior

## Why Testing Concurrent Code Matters

Concurrent code is notoriously hard to test. A test that passes 999 times and fails once is worse than a test that always fails -- it erodes trust in the test suite and wastes time investigating flakes. The root cause is almost always using `time.Sleep` instead of proper synchronization.

Deterministic concurrent tests use channels, WaitGroups, and barriers to control goroutine ordering. They verify behavior under controlled conditions rather than hoping for the right timing.

## The Problem

You have a concurrent worker pool that processes jobs from a channel. Your task is to write a comprehensive test suite that:

1. Verifies all jobs are processed exactly once
2. Confirms workers run concurrently (not sequentially)
3. Tests graceful shutdown behavior
4. Handles timeouts without `time.Sleep`
5. Passes reliably under `-race` with `-count=100`

## Requirements

1. **No time.Sleep** -- all tests must use synchronization primitives, not sleep-based timing
2. **Completion assertion** -- verify all N jobs complete using a WaitGroup or channel counter
3. **Concurrency proof** -- prove workers run concurrently by having each worker signal a barrier and showing all workers are active simultaneously
4. **Timeout assertion** -- use `select` with `time.After` only as a test deadline (not as synchronization)
5. **Ordering test** -- verify that results are collected regardless of completion order
6. **Stress test** -- run with `-count=100` and `-race` to verify no flakes or races
7. **Error propagation** -- test that a failing job is reported correctly without blocking other jobs

## Hints

<details>
<summary>Hint 1: Channel-based completion assertion</summary>

```go
func TestAllJobsComplete(t *testing.T) {
    const numJobs = 100
    done := make(chan struct{}, numJobs)

    pool := NewWorkerPool(4, func(job Job) {
        // process job
        done <- struct{}{}
    })
    pool.Start()

    for i := 0; i < numJobs; i++ {
        pool.Submit(Job{ID: i})
    }

    for i := 0; i < numJobs; i++ {
        select {
        case <-done:
            // job completed
        case <-time.After(5 * time.Second):
            t.Fatalf("timeout waiting for job %d", i)
        }
    }
}
```

</details>

<details>
<summary>Hint 2: Proving concurrency with a barrier</summary>

```go
func TestWorkersRunConcurrently(t *testing.T) {
    const numWorkers = 4
    atBarrier := make(chan struct{}, numWorkers)
    release := make(chan struct{})

    pool := NewWorkerPool(numWorkers, func(job Job) {
        atBarrier <- struct{}{} // signal arrival at barrier
        <-release                // wait for release
    })
    pool.Start()

    for i := 0; i < numWorkers; i++ {
        pool.Submit(Job{ID: i})
    }

    // Wait for all workers to reach the barrier
    for i := 0; i < numWorkers; i++ {
        select {
        case <-atBarrier:
        case <-time.After(5 * time.Second):
            t.Fatalf("only %d/%d workers reached barrier", i, numWorkers)
        }
    }
    // All workers are running concurrently -- release them
    close(release)
}
```

</details>

<details>
<summary>Hint 3: Helper for timed assertions</summary>

```go
func waitFor(t *testing.T, ch <-chan struct{}, msg string) {
    t.Helper()
    select {
    case <-ch:
    case <-time.After(5 * time.Second):
        t.Fatalf("timeout: %s", msg)
    }
}
```

</details>

<details>
<summary>Hint 4: The worker pool</summary>

```go
type Job struct {
    ID int
}

type WorkerPool struct {
    workers  int
    jobs     chan Job
    handler  func(Job)
    wg       sync.WaitGroup
}

func NewWorkerPool(workers int, handler func(Job)) *WorkerPool {
    return &WorkerPool{
        workers: workers,
        jobs:    make(chan Job, workers*2),
        handler: handler,
    }
}

func (p *WorkerPool) Start() {
    for i := 0; i < p.workers; i++ {
        p.wg.Add(1)
        go func() {
            defer p.wg.Done()
            for job := range p.jobs {
                p.handler(job)
            }
        }()
    }
}

func (p *WorkerPool) Submit(job Job) { p.jobs <- job }
func (p *WorkerPool) Shutdown()      { close(p.jobs); p.wg.Wait() }
```

</details>

## Verification

```bash
# Run once with race detector
go test -v -race ./...

# Run 100 times to verify no flakes
go test -race -count=100 ./...
```

Your tests should:
- Pass 100% of the time with `-count=100`
- Contain zero `time.Sleep` calls
- Use `time.After` only as test deadlines inside `select`
- Demonstrate all four test patterns: completion, concurrency proof, timeout, and error propagation

## What's Next

Continue to [04 - Deadlock Detection Strategies](../04-deadlock-detection-strategies/04-deadlock-detection-strategies.md) to learn how to detect and prevent deadlocks.

## Summary

- Never use `time.Sleep` for synchronization in tests; use channels, WaitGroups, and barriers
- Use `select` with `time.After` only as a deadline to prevent tests from hanging forever
- Prove concurrency by using a barrier pattern that requires all workers to be active simultaneously
- Run tests with `-race -count=100` to catch flakes and races
- Channel-based assertions are deterministic and portable across machines with different speeds

## Reference

- [Testing concurrent code in Go](https://go.dev/blog/race-detector)
- [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup)
- [testing package](https://pkg.go.dev/testing)
