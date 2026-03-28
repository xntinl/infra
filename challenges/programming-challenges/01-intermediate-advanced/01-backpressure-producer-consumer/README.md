# 1. Backpressure-Aware Producer-Consumer Pipeline

<!--
difficulty: intermediate-advanced
category: concurrency-fundamentals
languages: [go]
concepts: [channels, goroutines, backpressure, pipelines, graceful-shutdown]
estimated_time: 3-4 hours
bloom_level: analyze
prerequisites: [go-basics, goroutines, channels, select-statement, context-package]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines and channel mechanics (buffered and unbuffered)
- The `select` statement and `context` package for cancellation
- Basic understanding of producer-consumer patterns
- Familiarity with `sync.WaitGroup` and `sync.Mutex`

## Learning Objectives

- **Analyze** how buffered channel capacity creates natural backpressure between pipeline stages
- **Implement** a multi-stage pipeline with independent concurrency at each stage
- **Design** lossy and batch processing modes that handle overflow without blocking producers
- **Evaluate** pipeline throughput and per-stage latency through runtime metrics collection
- **Apply** graceful shutdown patterns that drain in-flight messages before exiting

## The Challenge

Every real system has components that produce data faster than downstream consumers can process it. A logging pipeline ingests millions of events per second but the database can only write thousands. An image processing service receives uploads faster than it can resize them. Without backpressure, these systems either run out of memory (unbounded queues), drop data silently, or deadlock.

Your task is to build a multi-stage processing pipeline in Go where each stage communicates through channels and naturally applies backpressure when overwhelmed. When a downstream stage is slow, upstream stages must slow down proportionally rather than buffering indefinitely.

Beyond basic backpressure, the pipeline must support two overflow strategies: **lossy mode** (drop the oldest message when the buffer is full) and **batch mode** (aggregate multiple messages into one before forwarding). It must also expose per-stage metrics (messages processed, throughput, average latency) and shut down gracefully, draining all in-flight messages before the process exits.

## Requirements

1. Implement a `Pipeline` type that chains N processing stages, each running in its own goroutine
2. Each stage receives input from a channel, applies a transformation function, and sends the result to the next stage's input channel
3. Buffered channels between stages create natural backpressure: when a stage's output channel is full, that stage blocks until the downstream consumer reads
4. Support adding stages dynamically before the pipeline starts (builder pattern)
5. Implement **lossy mode**: when a stage's output buffer is full, drop the oldest item and enqueue the new one
6. Implement **batch mode**: a stage accumulates N items (or waits a max duration), then forwards them as a single batch
7. Collect per-stage metrics: total messages processed, messages dropped (lossy mode), throughput (messages/second), average processing latency
8. Graceful shutdown via `context.Context`: when cancelled, each stage finishes processing its current item, drains remaining items from its input channel, then closes its output channel
9. The pipeline must not leak goroutines: every goroutine must exit after shutdown completes
10. Provide a `Run()` method that starts all stages and blocks until shutdown completes

## Hints

<details>
<summary>Hint 1: Pipeline stage structure</summary>

Each stage wraps a processing function and owns its input/output channels. Think of a stage as:

```go
type Stage[In, Out any] struct {
    name    string
    fn      func(In) Out
    in      <-chan In
    out     chan<- Out
    bufSize int
    metrics *StageMetrics
}
```

The pipeline connects stages by making one stage's output channel the next stage's input channel.
</details>

<details>
<summary>Hint 2: Lossy mode with ring buffer semantics</summary>

For lossy mode, use a `select` with `default` to detect a full channel, then drain one item before sending:

```go
select {
case out <- item:
    // sent successfully
default:
    <-out       // drop oldest
    out <- item // enqueue new
}
```

This gives you ring-buffer-like behavior on a standard buffered channel.
</details>

<details>
<summary>Hint 3: Batch mode with timer</summary>

Use a `time.Ticker` or `time.After` to flush incomplete batches:

```go
var batch []T
timer := time.NewTimer(maxWait)
for {
    select {
    case item := <-in:
        batch = append(batch, item)
        if len(batch) >= batchSize {
            out <- batch
            batch = nil
            timer.Reset(maxWait)
        }
    case <-timer.C:
        if len(batch) > 0 {
            out <- batch
            batch = nil
        }
        timer.Reset(maxWait)
    }
}
```
</details>

<details>
<summary>Hint 4: Graceful shutdown drain pattern</summary>

When context is cancelled, stop accepting new work but drain the input channel:

```go
case <-ctx.Done():
    // Drain remaining items
    for item := range in {
        result := process(item)
        out <- result
    }
    close(out)
    return
```

Upstream stages must close their output channels when they finish draining so downstream stages' `range` loops terminate.
</details>

## Acceptance Criteria

- [ ] Pipeline chains 3+ stages with different processing speeds
- [ ] Slow downstream stages cause upstream stages to block (backpressure visible in metrics)
- [ ] Lossy mode drops oldest messages when buffer is full, counter tracks drops
- [ ] Batch mode aggregates items and flushes on size threshold or timeout
- [ ] Per-stage metrics report: processed count, dropped count, throughput, avg latency
- [ ] Context cancellation triggers graceful shutdown with full drain
- [ ] Zero goroutine leaks after shutdown (verifiable with `runtime.NumGoroutine()`)
- [ ] Pipeline processes at least 100,000 messages without deadlock in tests

## Research Resources

- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines) -- foundational patterns for channel-based pipelines
- [Go Concurrency Patterns: Context](https://go.dev/blog/context) -- cancellation propagation through `context.Context`
- [Reactive Streams Specification](https://www.reactive-streams.org/) -- formal backpressure semantics (JVM-centric but the concepts transfer)
- [Sameer Ajmani: Advanced Go Concurrency Patterns (YouTube)](https://www.youtube.com/watch?v=QDDwwePbDtw) -- pipeline teardown and fan-out/fan-in
- [Mechanical Sympathy: Producer-Consumer Queues](https://mechanical-sympathy.blogspot.com/) -- hardware-aware queue design principles
