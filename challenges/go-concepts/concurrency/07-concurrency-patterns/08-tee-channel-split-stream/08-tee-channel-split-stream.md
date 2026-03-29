# 8. Tee-Channel: Split Stream

<!--
difficulty: advanced
concepts: [tee channel, stream splitting, nil-channel select, backpressure, data duplication]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, channels, select, done channel pattern, pipeline]
-->

## Prerequisites
- Go 1.22+ installed
- Strong understanding of goroutines, channels, and `select`
- Familiarity with the done-channel and pipeline patterns (exercises 01, 06)

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** a tee function that duplicates a channel stream into two outputs
- **Explain** the nil-channel select trick for ensuring both outputs receive each value
- **Analyze** how backpressure from a slow consumer affects the entire tee
- **Apply** stream splitting for parallel processing of the same data

## Why Tee-Channel
The tee-channel pattern takes one input stream and duplicates it into two output streams. Every value from the input appears in both outputs. This is analogous to the Unix `tee` command, which reads from stdin and writes to both stdout and a file simultaneously.

Use cases include: logging a data stream while also processing it; feeding the same data to two different analysis pipelines; duplicating events for both real-time processing and archival; splitting a stream for comparison (sending the same input through two different algorithms).

The challenge is backpressure. Since both output channels must receive every value, the tee runs at the speed of the slowest consumer. If one consumer is slow, the fast consumer also slows down because the tee cannot send the next value until both consumers have received the current one.

```
  Tee-Channel Data Flow

              +---> out1 (consumer A)
  input ----> |
              +---> out2 (consumer B)

  Every value goes to BOTH outputs.
  Speed = min(consumer A speed, consumer B speed)
```

## Step 1 -- Basic Tee Function with Nil-Channel Select

The nil-channel select pattern is the key technique. Here is how it works:

1. For each value from input, set `o1 = out1, o2 = out2` (both "armed")
2. Select: send to whichever consumer is ready first
3. Nil out the channel that received (`o1 = nil` or `o2 = nil`)
4. A nil channel blocks forever in select, so the next iteration MUST send to the other
5. After 2 sends, both consumers have the value

```go
package main

import (
    "fmt"
    "sync"
)

func tee(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int) {
    out1 := make(chan int)
    out2 := make(chan int)

    go func() {
        defer close(out1)
        defer close(out2)

        for val := range in {
            o1, o2 := out1, out2

            for count := 0; count < 2; count++ {
                select {
                case o1 <- val:
                    o1 = nil // sent to out1, nil it so next select goes to out2
                case o2 <- val:
                    o2 = nil // sent to out2, nil it so next select goes to out1
                case <-done:
                    return
                }
            }
        }
    }()

    return out1, out2
}

func main() {
    done := make(chan struct{})
    gen := make(chan int)
    go func() {
        defer close(gen)
        for i := 1; i <= 5; i++ {
            gen <- i
        }
    }()

    out1, out2 := tee(done, gen)
    var wg sync.WaitGroup
    wg.Add(2)
    go func() {
        defer wg.Done()
        for v := range out1 {
            fmt.Printf("  Consumer 1: %d\n", v)
        }
    }()
    go func() {
        defer wg.Done()
        for v := range out2 {
            fmt.Printf("  Consumer 2: %d\n", v)
        }
    }()
    wg.Wait()
    close(done)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: both consumers receive the same values:
```
  Consumer 1: 1
  Consumer 2: 1
  Consumer 1: 2
  Consumer 2: 2
  ...
```

## Step 2 -- Backpressure Demonstration

Show how a slow consumer affects the entire tee:

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func tee(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int) {
    out1 := make(chan int)
    out2 := make(chan int)
    go func() {
        defer close(out1)
        defer close(out2)
        for val := range in {
            o1, o2 := out1, out2
            for count := 0; count < 2; count++ {
                select {
                case o1 <- val:
                    o1 = nil
                case o2 <- val:
                    o2 = nil
                case <-done:
                    return
                }
            }
        }
    }()
    return out1, out2
}

func main() {
    done := make(chan struct{})
    defer close(done)

    gen := make(chan int)
    go func() {
        defer close(gen)
        for i := 1; i <= 5; i++ {
            fmt.Printf("  generator: sending %d at %v\n", i, time.Now().Format("04:05.000"))
            gen <- i
        }
    }()

    out1, out2 := tee(done, gen)
    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()
        for v := range out1 {
            fmt.Printf("  fast: got %d at %v\n", v, time.Now().Format("04:05.000"))
        }
    }()

    go func() {
        defer wg.Done()
        for v := range out2 {
            fmt.Printf("  slow: got %d at %v\n", v, time.Now().Format("04:05.000"))
            time.Sleep(200 * time.Millisecond) // slow consumer
        }
    }()

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
Observe that the fast consumer receives values at the same pace as the slow one. The timestamps reveal the bottleneck.

## Step 3 -- Practical Application: Log and Process

Build a pipeline that tees a stream for both logging and processing:

```go
package main

import (
    "fmt"
    "sync"
)

func tee(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int) {
    out1 := make(chan int)
    out2 := make(chan int)
    go func() {
        defer close(out1)
        defer close(out2)
        for val := range in {
            o1, o2 := out1, out2
            for count := 0; count < 2; count++ {
                select {
                case o1 <- val:
                    o1 = nil
                case o2 <- val:
                    o2 = nil
                case <-done:
                    return
                }
            }
        }
    }()
    return out1, out2
}

func main() {
    done := make(chan struct{})
    defer close(done)

    events := make(chan int)
    go func() {
        defer close(events)
        for i := 1; i <= 8; i++ {
            events <- i
        }
    }()

    logStream, processStream := tee(done, events)
    var wg sync.WaitGroup
    wg.Add(2)

    // Logger: records every event
    go func() {
        defer wg.Done()
        for event := range logStream {
            fmt.Printf("  [LOG] event=%d\n", event)
        }
    }()

    // Processor: only processes even events
    go func() {
        defer wg.Done()
        for event := range processStream {
            if event%2 == 0 {
                fmt.Printf("  [PROCESS] event=%d -> result=%d\n", event, event*event)
            }
        }
    }()

    wg.Wait()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: all events logged, only even events processed.

## Step 4 -- Three-Way Split (tee3)

Extend the pattern to split one input into three outputs:

```go
func tee3(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int, <-chan int) {
    out1 := make(chan int)
    out2 := make(chan int)
    out3 := make(chan int)

    go func() {
        defer close(out1)
        defer close(out2)
        defer close(out3)

        for val := range in {
            o1, o2, o3 := out1, out2, out3
            for count := 0; count < 3; count++ {
                select {
                case o1 <- val:
                    o1 = nil
                case o2 <- val:
                    o2 = nil
                case o3 <- val:
                    o3 = nil
                case <-done:
                    return
                }
            }
        }
    }()

    return out1, out2, out3
}
```

Same nil-channel technique but with `count < 3` and three channels.

## Common Mistakes

### Sending to Both Channels Without Coordination
**Wrong:**
```go
for val := range in {
    out1 <- val
    out2 <- val // blocks if out2 consumer is not ready
}
```
**What happens:** If `out2`'s consumer blocks, the send to `out1` in the next iteration also blocks, even if `out1`'s consumer is ready. Worse, there is no cancellation path.

**Fix:** Use `select` with nil-channel trick and done-channel, as shown in Step 1.

### Forgetting Done Channel in the Tee
**Wrong:**
```go
go func() {
    for val := range in {
        out1 <- val
        out2 <- val
    }
}()
```
**What happens:** If a consumer stops reading (context canceled, error, etc.), the tee goroutine blocks forever.

**Fix:** Always include `<-done` in select cases so the tee can exit when signaled.

### Closing Output Channels from the Consumer Side
Channels should be closed by the sender, not the receiver. The tee owns the output channels and closes them. Consumers should never close them.

## Verify What You Learned

Run `go run main.go` and verify:
- Basic tee: both consumers receive values 1-5
- Backpressure demo: fast consumer paced by slow consumer (timestamps prove it)
- Log and process: all events logged, only even events processed
- Tee3: all three consumers receive values 1-4
- Buffered tee: partial decoupling of consumer speeds

## What's Next
Continue to [09-rate-limiter-token-bucket](../09-rate-limiter-token-bucket/09-rate-limiter-token-bucket.md) to learn how to control the rate of work processing.

## Summary
- The tee-channel duplicates one input stream into two output streams
- Use the nil-channel select pattern to ensure both outputs receive each value
- The tee runs at the speed of the slowest consumer (backpressure)
- Add buffered intermediate channels to decouple fast and slow consumers
- Always include a `done` channel for cancellation to prevent goroutine leaks
- Common use cases: logging + processing, feeding parallel analysis, stream duplication

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/) -- tee-channel pattern
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
