---
difficulty: advanced
concepts: [errgroup pipeline, channel stages, producer-consumer, parallel workers, error propagation across stages]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [errgroup basics, errgroup WithContext, errgroup SetLimit, channels, context cancellation]
---

# 6. Errgroup Parallel Pipeline


## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a multi-stage pipeline where each stage is managed by an errgroup
- **Connect** pipeline stages using channels with proper lifecycle management
- **Propagate** errors across stages so a failure in any stage shuts down the entire pipeline
- **Apply** SetLimit to control parallelism within a pipeline stage

## Why Errgroup Pipelines

Real-world data processing often follows a pipeline pattern: produce data, process it in parallel, aggregate the results. Each stage can fail independently. Without errgroup, you need to manually coordinate goroutine shutdown, channel closing, and error propagation across stages -- a notoriously error-prone task.

By using errgroup with context, you get a clean architecture:
- Each stage is a goroutine (or set of goroutines) managed by a single errgroup
- A shared context connects all stages: when one fails, the context is cancelled, and all stages shut down
- Channels connect stages, with proper close semantics handled by the producing stage
- `SetLimit` controls parallelism within the processing stage

The pattern is: **Producer -> Channel -> Worker Pool -> Channel -> Aggregator**, all tied together with a single context.

## Step 1 -- Understand the Pipeline Architecture

The pipeline processes orders through three stages:

```
[Producer] --ordersCh--> [Worker Pool (bounded)] --resultsCh--> [Aggregator]
     |                           |                                    |
     +------ shared context (errgroup.WithContext) -------------------+
```

Run the program:

```bash
go mod tidy
go run main.go
```

You see orders flowing through all three stages, with workers processing in parallel.

## Step 2 -- Build the Producer Stage

The producer sends orders on a channel and closes it when done. The `defer close(out)` is critical -- without it, the workers' `range` loop never ends and the pipeline deadlocks.

```go
package main

import (
    "context"
    "fmt"
)

// Order represents an incoming order.
type Order struct {
    ID       int
    Customer string
    Amount   float64
}

func producer(ctx context.Context, orders []Order, out chan<- Order) error {
    defer close(out) // CRITICAL: signals workers that no more orders are coming

    for _, order := range orders {
        select {
        case <-ctx.Done():
            return ctx.Err() // pipeline cancelled -- stop producing
        case out <- order:
            fmt.Printf("  [producer] sent order %d (%s)\n", order.ID, order.Customer)
        }
    }
    return nil
}
```

Key points:
- `defer close(out)` at the top -- ensures the channel is closed even if the function returns early due to cancellation
- `select` on `ctx.Done()` -- allows the producer to stop if a downstream stage fails
- The producer is launched as a single goroutine in the errgroup

## Step 3 -- Build the Worker Pool

The worker pool is the parallelized stage. It uses an inner errgroup with `SetLimit` to control concurrency:

```go
package main

import (
    "context"
    "fmt"
    "math/rand"
    "time"

    "golang.org/x/sync/errgroup"
)

// ProcessedOrder is the result after validation and tax calculation.
type ProcessedOrder struct {
    ID       int
    Customer string
    Total    float64
    Status   string
}

func processorPool(ctx context.Context, numWorkers int, in <-chan Order, out chan<- ProcessedOrder) error {
    var g errgroup.Group
    g.SetLimit(numWorkers)

    for order := range in {
        order := order // capture for the closure
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
                fmt.Printf("  [worker] processed order %d: $%.2f\n", result.ID, result.Total)
            }
            return nil
        })
    }

    // Wait for ALL workers to finish, THEN close the output channel
    err := g.Wait()
    close(out) // safe: all workers are done
    return err
}

func processOrder(order Order) (ProcessedOrder, error) {
    time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
    if order.Amount < 0 {
        return ProcessedOrder{}, fmt.Errorf("invalid amount: %.2f", order.Amount)
    }
    total := order.Amount * 1.08 // 8% tax
    return ProcessedOrder{ID: order.ID, Customer: order.Customer, Total: total, Status: "completed"}, nil
}
```

Critical design decisions:
- **`SetLimit(numWorkers)`** ensures at most N orders are processed concurrently
- **`range in`** reads until the producer closes the input channel
- **`close(out)` comes AFTER `g.Wait()`** -- if you close before Wait, a worker that is still running will panic trying to send on a closed channel
- Each worker checks `ctx.Done()` before and after processing

## Step 4 -- Build the Aggregator

The aggregator collects results from the output channel:

```go
package main

import (
    "context"
    "fmt"
    "sync"
)

func aggregator(ctx context.Context, in <-chan ProcessedOrder, mu *sync.Mutex, results *[]ProcessedOrder) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case result, ok := <-in:
            if !ok {
                return nil // channel closed -- all workers are done
            }
            mu.Lock()
            *results = append(*results, result)
            mu.Unlock()
            fmt.Printf("  [aggregator] collected order %d\n", result.ID)
        }
    }
}
```

The aggregator reads until the channel is closed by the worker pool. It uses a mutex for the results slice because the results pointer is shared with the caller.

## Step 5 -- Wire Everything Together

The `runPipeline` function ties all stages together with a single errgroup:

```go
package main

import (
    "context"
    "fmt"
    "math/rand"
    "sync"
    "time"

    "golang.org/x/sync/errgroup"
)

type Order struct {
    ID       int
    Customer string
    Amount   float64
}

type ProcessedOrder struct {
    ID       int
    Customer string
    Total    float64
    Status   string
}

func main() {
    orders := []Order{
        {ID: 1, Customer: "Alice", Amount: 99.99},
        {ID: 2, Customer: "Bob", Amount: 149.50},
        {ID: 3, Customer: "Charlie", Amount: 29.99},
        {ID: 4, Customer: "Diana", Amount: 250.00},
        {ID: 5, Customer: "Eve", Amount: 75.00},
    }

    fmt.Printf("Processing %d orders with 3 workers...\n\n", len(orders))

    results, err := runPipeline(orders, 3)
    if err != nil {
        fmt.Printf("\nPipeline error: %v\n", err)
    }
    fmt.Printf("Processed: %d orders\n", len(results))
    var total float64
    for _, r := range results {
        fmt.Printf("  Order %d (%s): $%.2f\n", r.ID, r.Customer, r.Total)
        total += r.Total
    }
    fmt.Printf("Total revenue: $%.2f\n", total)
}

func runPipeline(orders []Order, numWorkers int) ([]ProcessedOrder, error) {
    g, ctx := errgroup.WithContext(context.Background())

    ordersCh := make(chan Order)
    resultsCh := make(chan ProcessedOrder)
    var mu sync.Mutex
    var results []ProcessedOrder

    // Stage 1: Producer
    g.Go(func() error {
        return producer(ctx, orders, ordersCh)
    })

    // Stage 2: Worker Pool
    g.Go(func() error {
        return processorPool(ctx, numWorkers, ordersCh, resultsCh)
    })

    // Stage 3: Aggregator
    g.Go(func() error {
        return aggregator(ctx, resultsCh, &mu, &results)
    })

    err := g.Wait()
    return results, err
}

func producer(ctx context.Context, orders []Order, out chan<- Order) error {
    defer close(out)
    for _, order := range orders {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case out <- order:
            fmt.Printf("  [producer]   sent order %d\n", order.ID)
        }
    }
    return nil
}

func processorPool(ctx context.Context, numWorkers int, in <-chan Order, out chan<- ProcessedOrder) error {
    var g errgroup.Group
    g.SetLimit(numWorkers)
    for order := range in {
        order := order
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
                fmt.Printf("  [worker]     processed order %d: $%.2f\n", result.ID, result.Total)
            }
            return nil
        })
    }
    err := g.Wait()
    close(out)
    return err
}

func aggregator(ctx context.Context, in <-chan ProcessedOrder, mu *sync.Mutex, results *[]ProcessedOrder) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case result, ok := <-in:
            if !ok {
                return nil
            }
            mu.Lock()
            *results = append(*results, result)
            mu.Unlock()
        }
    }
}

func processOrder(order Order) (ProcessedOrder, error) {
    time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
    if order.Amount < 0 {
        return ProcessedOrder{}, fmt.Errorf("invalid amount: %.2f", order.Amount)
    }
    return ProcessedOrder{
        ID: order.ID, Customer: order.Customer,
        Total: order.Amount * 1.08, Status: "completed",
    }, nil
}
```

**Expected output:**
```
Processing 5 orders with 3 workers...

  [producer]   sent order 1
  [producer]   sent order 2
  [producer]   sent order 3
  [worker]     processed order 1: $107.99
  [producer]   sent order 4
  ...
Processed: 5 orders
  Order 1 (Alice): $107.99
  Order 2 (Bob): $161.46
  ...
Total revenue: $XXX.XX
```

The beauty of this design:
- A single `errgroup.WithContext` manages all three stages
- If the producer fails, the context cancels and workers + aggregator shut down
- If a worker fails, the context cancels and the producer + aggregator shut down
- Channel close semantics propagate "done" signals: producer -> workers -> aggregator
- `g.Wait()` returns when all stages are done, with any error

## Error Propagation

To test error handling, add an order with a negative amount:

```go
orders := []Order{
    {ID: 1, Customer: "Alice", Amount: 99.99},
    {ID: 2, Customer: "Bob", Amount: -50.00}, // invalid -- triggers error
    {ID: 3, Customer: "Charlie", Amount: 29.99},
}
```

**Expected output:**
```
Pipeline error: processing order 2: invalid amount: -50.00
Partial results: N orders processed
```

The error from the worker propagates to the errgroup, which cancels the context. The producer stops sending, other workers bail out, and the aggregator stops collecting.

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

**What happens:** The workers' `range in` loop never ends. The pipeline deadlocks because nobody signals "no more data."

**Fix:** Always `defer close(out)` at the top of the producing function.

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

**What happens:** A worker that is still running sends on the closed channel -- panic.

**Fix:** Close AFTER Wait:
```go
err := g.Wait()
close(out) // safe: all workers are done
return err
```

### Not checking ctx.Done() in channel sends

**Wrong:**
```go
out <- result // blocks forever if aggregator is cancelled
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

### Using buffered channels to "fix" deadlocks

**Wrong:**
```go
ordersCh := make(chan Order, 1000) // big buffer to avoid blocking
```

**What happens:** Buffers mask the real problem. If a stage fails, the pipeline should shut down promptly. Large buffers mean the producer keeps filling the channel long after the context is cancelled, wasting work and memory.

**Fix:** Use unbuffered channels and proper `ctx.Done()` checks. Unbuffered channels enforce backpressure -- the producer cannot get ahead of the workers.

## Verify What You Learned

Run the full program and confirm:
1. The successful pipeline processes all 8 orders
2. The failing pipeline (with a negative amount) stops gracefully
3. Partial results are available even after an error
4. Workers run with bounded concurrency (3 at a time)

```bash
go run main.go
```

## Summary
- A pipeline is a series of stages connected by channels, each stage managed by goroutines
- Use `errgroup.WithContext` as the top-level coordinator: one context, one error boundary
- Each stage is a goroutine (or group of goroutines) in the errgroup
- Producers close their output channel when done (`defer close(out)`)
- Processor pools close their output channel AFTER `g.Wait()`, not before
- Always use `select` with `ctx.Done()` on channel operations to enable graceful shutdown
- `SetLimit` within a stage controls the degree of parallelism
- Errors in any stage cancel the context, triggering shutdown across all stages
- Use unbuffered channels for backpressure; large buffers mask design problems

## Reference
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [errgroup package documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Bryan Mills: Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
- [Go Concurrency Patterns: Context](https://go.dev/blog/context)
