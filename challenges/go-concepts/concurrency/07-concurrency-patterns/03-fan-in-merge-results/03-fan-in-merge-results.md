# 3. Fan-In: Merge Results

<!--
difficulty: intermediate
concepts: [fan-in, channel merging, WaitGroup, pipeline composition]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, fan-out pattern]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines, channels, and `sync.WaitGroup`
- Completion of exercises 01 (pipeline) and 02 (fan-out)

## Learning Objectives
After completing this exercise, you will be able to:
- **Merge** multiple channels into a single output channel
- **Implement** the fan-in function using goroutines and WaitGroup
- **Combine** fan-out and fan-in into a complete parallel processing pipeline
- **Recognize** when fan-in is the right pattern for aggregating concurrent results

## Why Fan-In
Fan-in is the complement of fan-out. Where fan-out distributes work across multiple workers, fan-in collects results from multiple producers into a single channel. Together, they form the classic scatter-gather pattern: split work, process in parallel, merge results.

The merge function is the core of fan-in. It takes N input channels, launches a goroutine per channel to forward values to a single output, and uses a WaitGroup to close the output when all inputs are drained. This is a recurring building block in Go systems -- from aggregating results of parallel API calls to combining log streams from multiple services.

Without fan-in, a consumer would need to select from all producer channels manually, which becomes unwieldy as the number of producers grows. The merge function abstracts this into a clean, reusable pattern.

## Step 1 -- Merge Two Channels

Start with the simplest case: merging exactly two channels into one.

Edit `main.go` and implement the `mergeTwo` function:

```go
func mergeTwo(a, b <-chan int) <-chan int {
    out := make(chan int)
    var wg sync.WaitGroup
    wg.Add(2)

    forward := func(ch <-chan int) {
        defer wg.Done()
        for v := range ch {
            out <- v
        }
    }

    go forward(a)
    go forward(b)

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

Each input channel gets its own forwarding goroutine. A third goroutine waits for both to finish and closes the output.

### Intermediate Verification
```bash
go run main.go
```
Expected: all values from both channels appear (order varies):
```
Merged (two): 1 2 3 10 20 30
```

## Step 2 -- Generalize to N Channels

Now implement a variadic `merge` that accepts any number of input channels:

```go
func merge(channels ...<-chan int) <-chan int {
    out := make(chan int)
    var wg sync.WaitGroup

    for _, ch := range channels {
        wg.Add(1)
        go func(c <-chan int) {
            defer wg.Done()
            for v := range c {
                out <- v
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

The pattern is identical to `mergeTwo` but works with a slice of channels. Each channel gets its own forwarding goroutine.

### Intermediate Verification
```bash
go run main.go
```
Expected: all values from three channels merged:
```
Merged (N): 1 2 3 10 20 30 100 200 300
```

## Step 3 -- Fan-Out + Fan-In Pipeline

Combine fan-out and fan-in into a complete parallel processing pipeline. Generate values, fan-out to multiple workers, and fan-in the results.

```go
func parallelPipeline() {
    fmt.Println("=== Parallel Pipeline ===")

    // Generator
    gen := func(nums ...int) <-chan int {
        out := make(chan int)
        go func() {
            for _, n := range nums {
                out <- n
            }
            close(out)
        }()
        return out
    }

    input := gen(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)

    // Fan-out: 3 workers, each with its own output channel
    numWorkers := 3
    workers := make([]<-chan int, numWorkers)
    for i := 0; i < numWorkers; i++ {
        workers[i] = squareWorker(i, input)
    }

    // Fan-in: merge all worker outputs
    results := merge(workers...)

    // Consume
    var total int
    for r := range results {
        total += r
    }
    fmt.Printf("  Sum of squares: %d\n\n", total)
}
```

Note: this fan-out approach shares one input channel across all workers (they compete for values). Each worker has its own output channel, and fan-in merges those outputs.

### Intermediate Verification
```bash
go run main.go
```
Expected: sum of squares of 1-10 = 385
```
=== Parallel Pipeline ===
  Sum of squares: 385
```

## Common Mistakes

### Closing Output Channel Inside the Forwarding Goroutine
**Wrong:**
```go
go func(c <-chan int) {
    for v := range c {
        out <- v
    }
    close(out) // other goroutines still sending!
}(ch)
```
**What happens:** The first goroutine to finish closes the channel, causing other goroutines to panic on send.

**Fix:** Close the output channel only once, after ALL forwarding goroutines complete. Use a WaitGroup and a dedicated closer goroutine.

### Forgetting to Pass the Channel Variable to the Goroutine
**Wrong:**
```go
for _, ch := range channels {
    wg.Add(1)
    go func() {
        defer wg.Done()
        for v := range ch { // captures loop variable
            out <- v
        }
    }()
}
```
**What happens:** All goroutines may read from the same (last) channel due to the closure capturing the loop variable.

**Fix:** Pass `ch` as a function argument: `go func(c <-chan int) { ... }(ch)`.

### Not Buffering the Output Channel When Needed
If all producers send simultaneously and the consumer is slow, an unbuffered output channel creates contention. Consider buffering if throughput matters, but remember that unbuffered channels provide natural backpressure.

## Verify What You Learned

Create a pipeline where three different generators produce different ranges (1-5, 6-10, 11-15), merge them with fan-in, then pass the merged stream through a `double` stage. Verify the output contains all 15 values doubled.

## What's Next
Continue to [04-worker-pool-fixed](../04-worker-pool-fixed/04-worker-pool-fixed.md) to build a fixed worker pool -- a structured combination of fan-out and fan-in.

## Summary
- Fan-in merges N channels into one using a forwarding goroutine per input
- The merge function uses WaitGroup to close the output only after all inputs are drained
- Fan-out + fan-in together form the scatter-gather pattern for parallel processing
- Always close the merged output in a separate goroutine that waits for all forwarders
- Pass channel variables explicitly to goroutines to avoid closure capture bugs
- The generalized `merge` function is a reusable building block for concurrent pipelines

## Reference
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
- [Effective Go: Channels of Channels](https://go.dev/doc/effective_go#chan_of_chan)
