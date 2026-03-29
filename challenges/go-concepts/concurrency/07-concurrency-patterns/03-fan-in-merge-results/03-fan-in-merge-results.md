---
difficulty: intermediate
concepts: [fan-in, channel merging, WaitGroup, pipeline composition]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, fan-out pattern]
---

# 3. Fan-In: Merge Results


## Learning Objectives
After completing this exercise, you will be able to:
- **Merge** multiple channels into a single output channel
- **Implement** the fan-in function using goroutines and WaitGroup
- **Combine** fan-out and fan-in into a complete parallel processing pipeline
- **Recognize** when fan-in is the right pattern for aggregating concurrent results

## Why Fan-In
Fan-in is the complement of fan-out. Where fan-out distributes work across multiple workers, fan-in collects results from multiple producers into a single channel. Together, they form the classic scatter-gather pattern: split work, process in parallel, merge results.

The merge function is the core of fan-in. It takes N input channels, launches a goroutine per channel to forward values to a single output, and uses a WaitGroup to close the output when all inputs are drained. This is a recurring building block in Go systems -- from aggregating results of parallel API calls to combining log streams from multiple services.

```
         Fan-In Data Flow
  ch-A ---+
           |
  ch-B ---+--> merged output --> consumer
           |
  ch-C ---+

  Each input gets a forwarding goroutine.
  A WaitGroup + closer goroutine closes
  the output after ALL inputs are drained.
```

## Step 1 -- Merge Two Channels

Start with the simplest case: merging exactly two channels into one.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func producer(name string, values ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, v := range values {
            fmt.Printf("  %s sending %d\n", name, v)
            out <- v
            time.Sleep(20 * time.Millisecond)
        }
        close(out)
    }()
    return out
}

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

func main() {
    a := producer("A", 1, 2, 3)
    b := producer("B", 10, 20, 30)
    fmt.Print("Merged: ")
    for v := range mergeTwo(a, b) {
        fmt.Printf("%d ", v)
    }
    fmt.Println()
}
```

Each input channel gets its own forwarding goroutine. A third goroutine waits for both to finish and closes the output.

### Intermediate Verification
```bash
go run main.go
```
Expected: all values from both channels appear (order varies):
```
Merged: 1 10 2 20 3 30
```

## Step 2 -- Generalize to N Channels

Now implement a variadic `merge` that accepts any number of input channels:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func producer(name string, values ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, v := range values {
            out <- v
            time.Sleep(20 * time.Millisecond)
        }
        close(out)
    }()
    return out
}

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

func main() {
    x := producer("X", 1, 2, 3)
    y := producer("Y", 10, 20, 30)
    z := producer("Z", 100, 200, 300)
    fmt.Print("Merged (N): ")
    for v := range merge(x, y, z) {
        fmt.Printf("%d ", v)
    }
    fmt.Println()
}
```

The pattern is identical to `mergeTwo` but works with a slice of channels. Each channel gets its own forwarding goroutine.

### Intermediate Verification
```bash
go run main.go
```
Expected: all values from three channels merged:
```
Merged (N): 1 10 100 2 20 200 3 30 300
```

## Step 3 -- Fan-Out + Fan-In Pipeline

Combine fan-out and fan-in into a complete parallel processing pipeline. Generate values, fan-out to multiple workers, and fan-in the results.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

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

func squareWorker(id int, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            result := n * n
            fmt.Printf("  worker %d: %d^2 = %d\n", id, n, result)
            out <- result
            time.Sleep(10 * time.Millisecond)
        }
        close(out)
    }()
    return out
}

func main() {
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

    // Fan-out: 3 workers share the input channel
    numWorkers := 3
    workers := make([]<-chan int, numWorkers)
    for i := 0; i < numWorkers; i++ {
        workers[i] = squareWorker(i, input)
    }

    // Fan-in: merge all worker outputs
    results := merge(workers...)

    var total int
    for r := range results {
        total += r
    }
    fmt.Printf("Sum of squares 1-10: %d\n", total) // 385
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: sum of squares of 1-10 = 385
```
  worker 0: 1^2 = 1
  ...
Sum of squares 1-10: 385
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

Run `go run main.go` and verify the output includes:
- Merge two: all 6 values from both channels
- Merge N: all 9 values from three channels
- Parallel pipeline: sum of squares 1-10 = 385
- Merge + double: 15 values doubled

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
