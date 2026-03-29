# 10. End-to-End Pipeline with Cancellation

<!--
difficulty: advanced
concepts: [pipeline, context cancellation, error handling, goroutine leak prevention, graceful shutdown]
tools: [go]
estimated_time: 45m
bloom_level: create
prerequisites: [goroutines, channels, select, context, sync.WaitGroup, pipeline, fan-out, fan-in, worker pool]
-->

## Prerequisites
- Go 1.22+ installed
- Completion of exercises 01-09 in this section
- Strong understanding of context cancellation, pipelines, fan-out/fan-in, and worker pools

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a complete multi-stage pipeline with context-driven cancellation
- **Handle** errors that propagate through pipeline stages
- **Prevent** goroutine leaks by ensuring all goroutines exit on cancellation
- **Verify** goroutine cleanup with runtime.NumGoroutine
- **Combine** multiple concurrency patterns into a production-quality system

## Why End-to-End Pipelines
Real-world concurrent systems are not single patterns -- they are compositions. A production pipeline combines generators, fan-out for parallelism, fan-in for aggregation, rate limiting, error handling, and cancellation. The challenge is not any single pattern but making them all work together correctly.

This exercise is the capstone of the concurrency patterns section. You will build a data processing pipeline that generates simulated records, processes them in parallel through multiple stages, handles errors at each stage, supports cancellation via context, and ensures zero goroutine leaks on shutdown. This is the architecture behind real systems like ETL pipelines, stream processors, and HTTP request handlers.

The key principle is: every goroutine must have a clear exit path. If any stage encounters an error or the context is canceled, all goroutines must terminate promptly. Leaked goroutines are a slow memory bleed that eventually kills long-running services.

## Step 1 -- Define the Data Pipeline Types

Start with clear types for the data flowing through the pipeline.

Edit `main.go` and verify the type definitions:

```go
type Record struct {
    ID   int
    Data string
}

type ProcessedRecord struct {
    Record   Record
    Result   string
    Stage    string
    Error    error
    Duration time.Duration
}
```

Each `ProcessedRecord` carries the original record, the processing result, which stage produced it, any error, and timing information. This traceability is essential for debugging pipelines.

### Intermediate Verification
The types compile. Proceed to Step 2.

## Step 2 -- Build the Generator with Context

Create a generator that produces records and respects context cancellation:

```go
func generateRecords(ctx context.Context, count int) <-chan Record {
    out := make(chan int)
    go func() {
        defer close(out)
        for i := 1; i <= count; i++ {
            record := Record{
                ID:   i,
                Data: fmt.Sprintf("record-%d", i),
            }
            select {
            case out <- record:
            case <-ctx.Done():
                fmt.Printf("  [generator] canceled at record %d: %v\n", i, ctx.Err())
                return
            }
        }
    }()
    return out
}
```

The generator checks `ctx.Done()` on every send. If the pipeline is canceled, it exits immediately.

### Intermediate Verification
```bash
go run main.go
```
The generator should produce records up to the count or until canceled.

## Step 3 -- Build Processing Stages

Create two processing stages: validate and transform. Each reads from an input channel, processes the record, and sends the result (including any error) downstream.

```go
func validate(ctx context.Context, in <-chan Record) <-chan ProcessedRecord {
    out := make(chan ProcessedRecord)
    go func() {
        defer close(out)
        for record := range in {
            start := time.Now()

            // Simulate validation: records with ID divisible by 7 are "invalid"
            var result ProcessedRecord
            if record.ID%7 == 0 {
                result = ProcessedRecord{
                    Record:   record,
                    Stage:    "validate",
                    Error:    fmt.Errorf("record %d failed validation", record.ID),
                    Duration: time.Since(start),
                }
            } else {
                time.Sleep(10 * time.Millisecond) // simulate work
                result = ProcessedRecord{
                    Record:   record,
                    Result:   fmt.Sprintf("valid(%s)", record.Data),
                    Stage:    "validate",
                    Duration: time.Since(start),
                }
            }

            select {
            case out <- result:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

func transform(ctx context.Context, in <-chan ProcessedRecord) <-chan ProcessedRecord {
    out := make(chan ProcessedRecord)
    go func() {
        defer close(out)
        for pr := range in {
            // Skip records that already have errors
            if pr.Error != nil {
                select {
                case out <- pr: // forward the error
                case <-ctx.Done():
                    return
                }
                continue
            }

            start := time.Now()
            time.Sleep(20 * time.Millisecond) // simulate heavier work

            result := ProcessedRecord{
                Record:   pr.Record,
                Result:   fmt.Sprintf("transformed(%s)", pr.Result),
                Stage:    "transform",
                Duration: time.Since(start),
            }

            select {
            case out <- result:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}
```

### Intermediate Verification
```bash
go run main.go
```
Records flow through validate -> transform. Records with ID%7==0 carry errors through the pipeline.

## Step 4 -- Fan-Out the Transform Stage

Parallelize the transform stage with multiple workers:

```go
func fanOutTransform(ctx context.Context, in <-chan ProcessedRecord, numWorkers int) <-chan ProcessedRecord {
    workers := make([]<-chan ProcessedRecord, numWorkers)
    for i := 0; i < numWorkers; i++ {
        workers[i] = transform(ctx, in)
    }
    return mergeProcessed(ctx, workers...)
}

func mergeProcessed(ctx context.Context, channels ...<-chan ProcessedRecord) <-chan ProcessedRecord {
    out := make(chan ProcessedRecord)
    var wg sync.WaitGroup

    for _, ch := range channels {
        wg.Add(1)
        go func(c <-chan ProcessedRecord) {
            defer wg.Done()
            for pr := range c {
                select {
                case out <- pr:
                case <-ctx.Done():
                    return
                }
            }
        }(ch)
    }

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

### Intermediate Verification
```bash
go run main.go
```
Multiple transform workers process records in parallel.

## Step 5 -- Collect Results and Handle Errors

Build the consumer that collects results, separates successes from errors, and provides a summary:

```go
func collect(in <-chan ProcessedRecord) (successes []ProcessedRecord, errors []ProcessedRecord) {
    for pr := range in {
        if pr.Error != nil {
            errors = append(errors, pr)
        } else {
            successes = append(successes, pr)
        }
    }
    return
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: summary showing success count, error count, and timing.

## Step 6 -- Cancellation and Goroutine Leak Check

Wire everything together with context cancellation and verify no goroutines leak:

```go
func runPipeline(totalRecords int, cancelAfter int) {
    fmt.Printf("=== Pipeline: %d records, cancel after %d ===\n", totalRecords, cancelAfter)

    goroutinesBefore := runtime.NumGoroutine()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Build pipeline
    records := generateRecords(ctx, totalRecords)
    validated := validate(ctx, records)
    transformed := fanOutTransform(ctx, validated, 3)

    // Optionally cancel midway
    if cancelAfter > 0 {
        go func() {
            count := 0
            for range transformed {
                count++
                if count >= cancelAfter {
                    fmt.Printf("  [pipeline] canceling after %d results\n", cancelAfter)
                    cancel()
                    return
                }
            }
        }()
        time.Sleep(500 * time.Millisecond) // let cancellation propagate
    } else {
        successes, errors := collect(transformed)
        fmt.Printf("  Successes: %d, Errors: %d\n", len(successes), len(errors))
        for _, e := range errors {
            fmt.Printf("    error: %v\n", e.Error)
        }
    }

    // Verify no goroutine leaks
    time.Sleep(100 * time.Millisecond) // let goroutines clean up
    goroutinesAfter := runtime.NumGoroutine()
    leaked := goroutinesAfter - goroutinesBefore
    if leaked > 0 {
        fmt.Printf("  WARNING: %d goroutine(s) leaked!\n", leaked)
    } else {
        fmt.Printf("  OK: no goroutine leaks (before=%d, after=%d)\n", goroutinesBefore, goroutinesAfter)
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: pipeline runs to completion with no leaks. Canceled pipeline also has no leaks.

## Common Mistakes

### Not Checking ctx.Done() in Every Stage
**Wrong:**
```go
for record := range in {
    out <- processRecord(record) // no cancellation check
}
```
**What happens:** Even after cancel is called, the stage continues processing until the input channel closes, wasting CPU.

**Fix:** Always wrap sends in `select { case out <- result: case <-ctx.Done(): return }`.

### Goroutine Leak from Unclosed Channels
**Wrong:**
```go
func stage(in <-chan Record) <-chan Record {
    out := make(chan Record)
    go func() {
        for r := range in {
            out <- r
        }
        // forgot close(out)
    }()
    return out
}
```
**What happens:** Downstream stages that `range` over this output block forever.

**Fix:** Always `defer close(out)` at the top of the goroutine.

### Not Propagating Errors Through the Pipeline
**Wrong:** Dropping errors silently.
```go
if err != nil {
    continue // error swallowed, consumer never knows
}
```
**Fix:** Wrap errors in the ProcessedRecord and forward them. Let the consumer decide how to handle errors.

### Using runtime.NumGoroutine Without a Settling Delay
After canceling, goroutines need a moment to respond to ctx.Done() and exit. Check the count after a brief sleep to avoid false positive leak reports.

## Verify What You Learned

Extend the pipeline with a rate-limiting stage between validate and transform. Use the token bucket pattern from exercise 09 to limit throughput to 20 records per second. Run with 50 records and verify the total processing time matches the expected rate-limited duration.

## What's Next
Congratulations on completing the concurrency patterns section. You now have the building blocks for any concurrent Go system. Continue to [08-errgroup](../../08-errgroup/) to learn the `errgroup` package for structured error handling in concurrent operations.

## Summary
- Production pipelines combine generators, fan-out, fan-in, error handling, and cancellation
- Every goroutine must check `ctx.Done()` on every blocking operation (send, receive)
- Errors should flow through the pipeline as data, not be silently dropped
- Use `runtime.NumGoroutine()` to verify zero goroutine leaks after shutdown
- Always `defer close(out)` in every stage goroutine
- Context cancellation propagates through all stages when any part of the pipeline signals stop
- The pipeline pattern scales: add stages, parallelize bottlenecks, compose patterns freely

## Reference
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Go Blog: Context](https://go.dev/blog/context)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/) -- comprehensive pattern reference
- [Advanced Go Concurrency Patterns](https://www.youtube.com/watch?v=QDDwwePbDtw)
