# 1. Pipeline Pattern

<!--
difficulty: intermediate
concepts: [pipeline, channel chaining, stage decomposition, goroutine composition]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, channel direction]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines and how to launch them
- Familiarity with channels (send, receive, close)
- Understanding of channel direction types (`<-chan`, `chan<-`)

## Learning Objectives
After completing this exercise, you will be able to:
- **Construct** a multi-stage pipeline where each stage is a goroutine
- **Connect** stages using channels as the data conduit
- **Recognize** how pipelines decompose complex processing into composable stages
- **Apply** the close-channel idiom to signal stage completion downstream

## Why Pipelines
A pipeline is a series of stages connected by channels, where each stage is a group of goroutines that receive values from an upstream channel, perform a transformation, and send the results to a downstream channel. This pattern is fundamental to Go concurrency because it decomposes complex work into small, testable, composable stages.

Pipelines are everywhere in real systems: data ingestion processes that read, validate, transform, and store records; HTTP middleware chains; image processing workflows; log processing systems. The key insight is that each stage runs concurrently -- while stage 2 processes item N, stage 1 can already be producing item N+1. This overlap is where pipelines earn their performance gains.

The pipeline pattern relies on a critical contract: when a stage is done producing values, it closes its output channel. This propagates a "done" signal downstream. Without this discipline, downstream stages would block forever waiting for values that will never arrive.

```
                 Pipeline Data Flow
  +-----------+     +---------+     +--------+     +----------+
  | generator | --> | square  | --> | filter | --> | consumer |
  +-----------+     +---------+     +--------+     +----------+
    (source)        (transform)     (filter)        (drain)
       |                |               |               |
    <-chan int       <-chan int      <-chan int        range
```

## Step 1 -- Build a Generator Stage

The first stage of any pipeline is a generator: a function that produces values and sends them into a channel. The generator takes the raw input, converts it into a stream, and returns a receive-only channel.

Edit `main.go` and examine the `generator` function:

```go
package main

import "fmt"

func generator(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, n := range nums {
            out <- n
        }
        close(out)
    }()
    return out
}

func main() {
    fmt.Print("Generator output: ")
    for n := range generator(2, 3, 4, 5) {
        fmt.Printf("%d ", n)
    }
    fmt.Println()
}
```

The function launches a goroutine that sends each value into the channel and closes it when done. The caller receives a `<-chan int` -- it can only read from it.

### Intermediate Verification
```bash
go run main.go
```
You should see the generator producing values:
```
Generator output: 2 3 4 5
```

## Step 2 -- Build a Transform Stage

Now examine the `square` stage. It reads integers from an input channel, squares them, and sends the results to an output channel. This is the middle of the pipeline.

```go
package main

import "fmt"

func generator(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, n := range nums {
            out <- n
        }
        close(out)
    }()
    return out
}

func square(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            out <- n * n
        }
        close(out)
    }()
    return out
}

func main() {
    fmt.Print("Squared output: ")
    for n := range square(generator(2, 3, 4, 5)) {
        fmt.Printf("%d ", n)
    }
    fmt.Println()
}
```

Notice the symmetry: every stage follows the same pattern -- accept a channel, return a channel, do work in a goroutine, close when done. This uniformity makes stages composable.

### Intermediate Verification
```bash
go run main.go
```
You should see squared values:
```
Squared output: 4 9 16 25
```

## Step 3 -- Chain the Pipeline

Connect generator and square into a pipeline and consume the final output. This is where the composition happens.

```go
package main

import "fmt"

func generator(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, n := range nums {
            out <- n
        }
        close(out)
    }()
    return out
}

func square(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            out <- n * n
        }
        close(out)
    }()
    return out
}

func main() {
    nums := generator(2, 3, 4, 5)
    squared := square(nums)

    fmt.Println("Pipeline output:")
    for result := range squared {
        fmt.Printf("  %d\n", result)
    }
}
```

The pipeline reads naturally left-to-right: generate -> square -> print. Each arrow is a channel. Each stage runs in its own goroutine. The consumer (the `range` loop) drives the pipeline by pulling values through.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Pipeline output:
  4
  9
  16
  25
```

## Step 4 -- Add a Filter Stage

Add a `filter` stage that only passes through values above a given threshold. This demonstrates that you can insert new stages anywhere in the pipeline without modifying existing ones.

```go
package main

import "fmt"

func generator(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        for _, n := range nums {
            out <- n
        }
        close(out)
    }()
    return out
}

func square(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            out <- n * n
        }
        close(out)
    }()
    return out
}

func filter(in <-chan int, threshold int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            if n > threshold {
                out <- n
            }
        }
        close(out)
    }()
    return out
}

func main() {
    nums := generator(2, 3, 4, 5)
    squared := square(nums)
    filtered := filter(squared, 10)

    fmt.Println("Filtered pipeline output:")
    for result := range filtered {
        fmt.Printf("  %d\n", result)
    }
}
```

Now chain it: generator -> square -> filter -> print.

### Intermediate Verification
```bash
go run main.go
```
With threshold 10, output should show only values > 10:
```
Filtered pipeline output:
  16
  25
```

## Common Mistakes

### Forgetting to Close the Output Channel

**Wrong:**
```go
package main

import "fmt"

func square(in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        for n := range in {
            out <- n * n
        }
        // forgot close(out) -- downstream blocks forever
    }()
    return out
}

func main() {
    in := make(chan int)
    go func() {
        in <- 2
        in <- 3
        close(in)
    }()
    for v := range square(in) {
        fmt.Println(v)
    }
    // deadlock: range never ends because out is never closed
}
```
**What happens:** The downstream `range` loop blocks forever waiting for more values. The program deadlocks.

**Fix:** Always close the output channel when the goroutine finishes producing values.

### Returning a Bidirectional Channel

**Wrong:**
```go
func generator(nums ...int) chan int { // bidirectional!
    out := make(chan int)
    go func() {
        for _, n := range nums {
            out <- n
        }
        close(out)
    }()
    return out
}
```
**What happens:** Callers could accidentally send values back into the channel, breaking the pipeline contract.

**Fix:** Return `<-chan int` (receive-only). Let the compiler enforce the data flow direction.

### Blocking the Generator on a Full Channel

If you use an unbuffered channel and the consumer is slow, the generator blocks on every send. This is actually correct behavior (backpressure), but if you need buffering, you can use `make(chan int, bufferSize)`. Be intentional about the choice.

## Verify What You Learned

Run `go run main.go` and verify the full output matches:

```
Exercise: Pipeline Pattern

=== Generator Only ===
Generator output: 2 3 4 5

=== Generator -> Square ===
Squared output: 4 9 16 25

=== Three-Stage Pipeline ===
Pipeline output:
  4
  9
  16
  25

=== Filtered Pipeline (threshold=10) ===
Filtered pipeline output:
  16
  25

=== Four-Stage Pipeline: gen -> square -> double -> filter(30) ===
Full pipeline output:
  32
  50

=== String Pipeline ===
String pipeline output:
  [HELLO]
  [WORLD]
  [GOPHER]
```

The string pipeline shows that the pattern works with any type, not just integers.

## What's Next
Continue to [02-fan-out-distribute-work](../02-fan-out-distribute-work/02-fan-out-distribute-work.md) to learn how to distribute work from a single channel across multiple workers.

## Summary
- A pipeline is a series of stages connected by channels, each stage running as a goroutine
- The generator pattern creates the first stage: a function that returns `<-chan T`
- Each stage reads from an input channel, transforms values, and sends to an output channel
- Closing the output channel is mandatory to signal completion downstream
- Stages are composable: new stages can be inserted without modifying existing ones
- Pipeline stages run concurrently, enabling overlap between production and consumption

## Reference
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
