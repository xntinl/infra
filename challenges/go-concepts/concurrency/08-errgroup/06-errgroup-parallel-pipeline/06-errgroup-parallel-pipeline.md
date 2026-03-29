# 6. Errgroup Parallel Pipeline

<!--
difficulty: advanced
concepts: [errgroup pipeline, channel stages, producer-consumer, parallel workers, error propagation across stages]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [errgroup basics, errgroup WithContext, errgroup SetLimit, channels, context cancellation]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-05 of this section
- Understanding of channel-based pipelines (send on one channel, receive on another)
- Familiarity with producer-consumer pattern
- Context cancellation and cooperative shutdown

## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a multi-stage pipeline where each stage is managed by an errgroup
- **Connect** pipeline stages using channels with proper lifecycle management
- **Propagate** errors across stages so a failure in any stage shuts down the entire pipeline
- **Apply** SetLimit to control parallelism within a pipeline stage

## Why Errgroup Pipelines
Real-world data processing often follows a pipeline pattern: produce data, process it in parallel, aggregate the results. Each stage can fail independently. Without errgroup, you need to manually coordinate goroutine shutdown, channel closing, and error propagation across stages -- a notoriously error-prone task.

By using errgroup with context, you get a clean architecture:
- Each stage is a set of goroutines managed by a single errgroup
- A shared context connects all stages: when one fails, the context is cancelled, and all stages shut down
- Channels connect stages, with proper close semantics handled by the producing stage
- `SetLimit` controls parallelism within the processing stage

The pattern is: **Producer -> Channel -> Worker Pool -> Channel -> Aggregator**, all tied together with a single context.

## Step 1 -- Understand the Pipeline Architecture

Run the starter code:

```bash
go mod tidy
go run main.go
```

The program outlines a three-stage pipeline:
1. **Producer**: Generates work items and sends them on a channel
2. **Processor Pool**: Multiple workers read from the input channel, process items, and send results on an output channel
3. **Aggregator**: Reads from the output channel and collects final results

Study the `Order` and `ProcessedOrder` types in `main.go`. The pipeline simulates order processing: receive orders, validate and enrich them in parallel, then aggregate statistics.

### Intermediate Verification
The program compiles and shows the pipeline structure. The stages are stubbed out.

## Step 2 -- Build the Producer Stage

Implement the `producer` function that generates orders and sends them on a channel:

```go
func producer(ctx context.Context, orders []Order, out chan<- Order) error {
    defer close(out) // CRITICAL: close channel when done producing

    for _, order := range orders {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case out <- order:
            fmt.Printf("  [producer] sent order %d\n", order.ID)
        }
    }
    return nil
}
```

Key points:
- The producer closes the output channel when done (or on error). This signals downstream stages that no more items are coming.
- The `select` on `ctx.Done()` allows the producer to stop if the pipeline is cancelled.
- The producer is launched as a single goroutine within the errgroup.

### Intermediate Verification
Add the producer to the pipeline and verify that orders flow into the channel. The processor and aggregator can be stubs that drain the channel.

## Step 3 -- Build the Processor Pool

Implement the `processorPool` function. This is the parallelized stage with multiple workers:

```go
func processorPool(ctx context.Context, numWorkers int, in <-chan Order, out chan<- ProcessedOrder) error {
    var g errgroup.Group
    g.SetLimit(numWorkers)

    for order := range in {
        order := order // capture
        g.Go(func() error {
            select {
            case <-ctx.Done():
                return ctx.Err()
            default:
            }

            result, err := processOrder(order)
            if err != nil {
                return fmt.Errorf("processing order %d: %w", order.ID, err)
            }

            select {
            case <-ctx.Done():
                return ctx.Err()
            case out <- result:
            }
            return nil
        })
    }

    // Wait for all workers to finish before closing the output channel
    err := g.Wait()
    close(out) // signal aggregator that no more results are coming
    return err
}
```

Key design decisions:
- `SetLimit(numWorkers)` ensures at most N orders are processed concurrently
- The `range in` loop reads until the producer closes the input channel
- The output channel is closed AFTER `g.Wait()` -- only when all workers are done
- Each worker checks `ctx.Done()` for pipeline cancellation

### Intermediate Verification
```bash
go run main.go
```
Orders flow from producer through the worker pool. Workers process items in parallel, limited by SetLimit.

## Step 4 -- Build the Aggregator

Implement the `aggregator` function that collects results:

```go
func aggregator(ctx context.Context, in <-chan ProcessedOrder, results *[]ProcessedOrder) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case result, ok := <-in:
            if !ok {
                return nil // channel closed, all results collected
            }
            *results = append(*results, result)
            fmt.Printf("  [aggregator] collected order %d (total: $%.2f)\n",
                result.ID, result.Total)
        }
    }
}
```

The aggregator reads from the results channel until it is closed by the processor pool. It appends each result to the shared results slice (safe because only one goroutine runs the aggregator).

### Intermediate Verification
```bash
go run main.go
```
The full pipeline runs: producer -> processors -> aggregator. Results are collected and printed.

## Step 5 -- Wire Everything Together with a Shared Context

The `runPipeline` function ties all stages together:

```go
func runPipeline(orders []Order, numWorkers int) ([]ProcessedOrder, error) {
    g, ctx := errgroup.WithContext(context.Background())

    ordersCh := make(chan Order)
    resultsCh := make(chan ProcessedOrder)
    var results []ProcessedOrder

    // Stage 1: Producer
    g.Go(func() error {
        return producer(ctx, orders, ordersCh)
    })

    // Stage 2: Processor Pool
    g.Go(func() error {
        return processorPool(ctx, numWorkers, ordersCh, resultsCh)
    })

    // Stage 3: Aggregator
    g.Go(func() error {
        return aggregator(ctx, resultsCh, &results)
    })

    if err := g.Wait(); err != nil {
        return results, err // return partial results + error
    }

    return results, nil
}
```

The beauty of this design:
- A single `errgroup.WithContext` manages all three stages
- If the producer fails, the context is cancelled and workers + aggregator shut down
- If a worker fails, the context is cancelled and the producer + aggregator shut down
- Channel close semantics propagate "done" signals from producer -> workers -> aggregator
- `g.Wait()` returns when all stages are done, with any error that occurred

### Intermediate Verification
```bash
go run main.go
```
Run with normal orders to see success. Then add an invalid order to trigger an error and observe the pipeline shutting down gracefully.

## Common Mistakes

### Forgetting to close channels
**Wrong:**
```go
func producer(ctx context.Context, out chan<- Order) error {
    for _, o := range orders {
        out <- o
    }
    // forgot close(out)!
    return nil
}
```
**What happens:** The processor stage's `range in` loop never terminates. The pipeline deadlocks.

**Fix:** Always `defer close(out)` at the beginning of the producing function.

### Closing the output channel before Wait returns
**Wrong:**
```go
func processorPool(..., out chan<- ProcessedOrder) error {
    for order := range in {
        g.Go(func() error {
            out <- result
            return nil
        })
    }
    close(out) // BUG: workers may still be sending!
    return g.Wait()
}
```
**What happens:** Workers send on a closed channel -- panic.

**Fix:** Close the channel AFTER `g.Wait()`:
```go
err := g.Wait()
close(out) // safe: all workers are done
return err
```

### Not checking ctx.Done() in channel sends
**Wrong:**
```go
case out <- result: // blocks forever if aggregator is cancelled
```
**What happens:** If the aggregator stops reading (due to cancellation), this send blocks forever. The pipeline deadlocks instead of shutting down.

**Fix:** Always pair channel sends with `ctx.Done()`:
```go
select {
case <-ctx.Done():
    return ctx.Err()
case out <- result:
}
```

### Using unbuffered channels without context checks
**Wrong:**
```go
ordersCh := make(chan Order) // unbuffered
// producer sends, but if processor is cancelled, no one reads
```
**What happens:** The producer blocks on send, the context is cancelled but the producer never checks it because it is stuck in the channel send.

**Fix:** Always use `select` with `ctx.Done()` when sending on channels in a pipeline.

## Verify What You Learned

Extend the pipeline with a fourth stage: a "reporter" that receives aggregated results and generates a summary report. The reporter should:
1. Calculate total revenue, average order value, and count of processed orders
2. Print the report after all results are collected
3. Properly shut down on context cancellation

## What's Next
You have completed all errgroup exercises. You now understand the full spectrum: from basic group coordination to complex multi-stage pipelines. Consider revisiting the [concurrency patterns](../07-concurrency-patterns/) section to see how errgroup integrates with other patterns like fan-out/fan-in and worker pools.

## Summary
- A pipeline is a series of stages connected by channels, each stage managed by goroutines
- Use `errgroup.WithContext` as the top-level coordinator: one context, one error boundary
- Each stage is a goroutine (or group of goroutines) in the errgroup
- Producers close their output channel when done (`defer close(out)`)
- Processor pools close their output channel AFTER `g.Wait()`, not before
- Always use `select` with `ctx.Done()` on channel operations to enable graceful shutdown
- `SetLimit` within a stage controls the degree of parallelism
- Errors in any stage cancel the context, triggering shutdown across all stages

## Reference
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Bryan Mills: Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
- [Go Concurrency Patterns: Context](https://go.dev/blog/context)
