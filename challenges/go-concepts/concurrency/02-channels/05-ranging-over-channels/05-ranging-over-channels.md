# 5. Ranging Over Channels

<!--
difficulty: intermediate
concepts: [range, close, channel-iteration, deadlock, producer-consumer]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, unbuffered-channels, buffered-channels, channel-direction]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-04 (channels basics through direction)
- Understanding of `close()` on channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Iterate** over channel values using `for range`
- **Explain** why `close()` is required for range to terminate
- **Diagnose** deadlocks caused by missing `close()` calls
- **Apply** the producer-closes, consumer-ranges pattern

## Why Range Over Channels

When consuming all values from a channel, you could write a manual loop with the comma-ok idiom. But `for range` on a channel is cleaner: it receives values one at a time and automatically exits when the channel is closed and drained.

This creates a clean separation of responsibilities. The producer decides when to stop (by closing the channel). The consumer just ranges -- it doesn't need to know how many values to expect. This decoupling is essential for pipelines where the number of values isn't known in advance.

The critical contract is: **the producer must close the channel when done**. If it doesn't, the range loop blocks forever, waiting for more values. This is the most common source of deadlocks with range loops.

## Step 1 -- Basic Range Over a Channel

Range over a channel that receives a known number of values.

```go
package main

import "fmt"

func main() {
    ch := make(chan int)

    go func() {
        for i := 1; i <= 5; i++ {
            ch <- i
        }
        close(ch) // REQUIRED: tells range there are no more values
    }()

    // for val := range ch receives values until the channel is closed AND
    // all buffered values are consumed. Then it exits cleanly.
    for val := range ch {
        fmt.Println(val)
    }
    fmt.Println("Channel fully drained")
}
```

### Verification
```bash
go run main.go
# Expected:
#   1
#   2
#   3
#   4
#   5
#   Channel fully drained
```

What if you remove `close(ch)`? The range loop receives 1-5, then blocks forever waiting for more. Go's runtime detects the deadlock.

## Step 2 -- Deadlock Without Close

This program intentionally omits `close()` to demonstrate the deadlock.

```go
package main

import "fmt"

func main() {
    ch := make(chan int)

    go func() {
        for i := 1; i <= 3; i++ {
            ch <- i
        }
        // Oops, forgot to close!
    }()

    for val := range ch { // blocks forever after receiving 3 values
        fmt.Println(val)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   1
#   2
#   3
#   fatal error: all goroutines are asleep - deadlock!
```

The range loop received 1, 2, 3, then waits for more. The goroutine has exited. No one will ever send again or close the channel. Deadlock.

## Step 3 -- Range with Buffered Channels

Range works identically with buffered channels. The key insight: close + range drains all remaining buffered values before exiting.

```go
package main

import "fmt"

func main() {
    ch := make(chan string, 3)

    // No goroutine needed -- buffer holds all three values.
    ch <- "alpha"
    ch <- "beta"
    ch <- "gamma"
    close(ch) // close with values still in buffer

    // range consumes all three buffered values, then exits.
    for val := range ch {
        fmt.Println(val)
    }
    fmt.Println("All buffered values consumed after close")
}
```

### Verification
```bash
go run main.go
# Expected:
#   alpha
#   beta
#   gamma
#   All buffered values consumed after close
```

## Step 4 -- Producer Closes, Consumer Ranges (Fibonacci)

The canonical pattern: a producer function that owns the channel's lifecycle.

```go
package main

import "fmt"

func fibonacci(n int) <-chan int {
    ch := make(chan int)
    go func() {
        a, b := 0, 1
        for i := 0; i < n; i++ {
            ch <- a
            a, b = b, a+b
        }
        close(ch) // producer closes when done
    }()
    return ch
}

func main() {
    // The consumer just ranges -- no need to know the count.
    for num := range fibonacci(10) {
        fmt.Printf("%d ", num)
    }
    fmt.Println()
}
```

### Verification
```bash
go run main.go
# Expected: 0 1 1 2 3 5 8 13 21 34
```

## Step 5 -- Pipeline with Range

Chain range-based stages into a pipeline. Each stage reads until its input closes, processes, and closes its output.

```go
package main

import "fmt"

func square(in <-chan int, out chan<- int) {
    for val := range in {
        out <- val * val
    }
    close(out)
}

func main() {
    nums := make(chan int)
    go func() {
        for i := 1; i <= 5; i++ {
            nums <- i
        }
        close(nums)
    }()

    squared := make(chan int)
    go square(nums, squared)

    for val := range squared {
        fmt.Println("Squared:", val)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   Squared: 1
#   Squared: 4
#   Squared: 9
#   Squared: 16
#   Squared: 25
```

## Step 6 -- Word Frequency Counter

A practical pipeline: lines become words, words are counted.

```go
package main

import (
    "fmt"
    "strings"
)

func generateLines() <-chan string {
    ch := make(chan string)
    go func() {
        lines := []string{
            "go channels are powerful",
            "channels make concurrency safe",
            "go is powerful and safe",
        }
        for _, line := range lines {
            ch <- line
        }
        close(ch)
    }()
    return ch
}

func extractWords(lines <-chan string) <-chan string {
    ch := make(chan string)
    go func() {
        for line := range lines {
            for _, word := range strings.Fields(line) {
                ch <- word
            }
        }
        close(ch)
    }()
    return ch
}

func main() {
    words := extractWords(generateLines())

    freq := make(map[string]int)
    for word := range words {
        freq[word]++
    }

    for word, count := range freq {
        fmt.Printf("  %s: %d\n", word, count)
    }
}
```

### Verification
```bash
go run main.go
# Expected (order may vary):
#   go: 2
#   channels: 2
#   powerful: 2
#   safe: 2
#   are: 1
#   make: 1
#   concurrency: 1
#   is: 1
#   and: 1
```

## Common Mistakes

### Consumer Closing the Channel

**Wrong:**
```go
package main

import "fmt"

func main() {
    ch := make(chan int, 5)
    go func() {
        for i := 0; i < 5; i++ {
            ch <- i
        }
    }()

    for val := range ch {
        fmt.Println(val)
        if val == 4 {
            close(ch) // consumer should not close!
        }
    }
}
```

**What happens:** If the producer tries to send after the consumer closes, it panics: `send on closed channel`.

**Correct:** Only the producer (sender) should close a channel. The consumer ranges and trusts the producer to close.

### Multiple Goroutines Closing the Same Channel

**Wrong:**
```go
ch := make(chan int)
for i := 0; i < 3; i++ {
    go func() {
        ch <- 1
        close(ch) // second close panics!
    }()
}
```

**What happens:** The second goroutine to call `close()` causes a panic: `close of closed channel`.

**Fix:** Coordinate so that only one goroutine closes the channel. Use `sync.WaitGroup` to wait for all senders, then close once.

## What's Next
Continue to [06-closing-channels](../06-closing-channels/06-closing-channels.md) to deep-dive into close semantics, the comma-ok idiom, and broadcasting.

## Summary
- `for val := range ch` receives values until the channel is closed and empty
- The producer must call `close(ch)` -- without it, range blocks forever (deadlock)
- Range on a closed buffered channel drains all remaining values before exiting
- Convention: the producer (sender) closes the channel; the consumer (receiver) ranges
- Never close a channel from the receive side -- it risks panic on the send side
- Never close a channel more than once -- the second close panics

## Reference
- [A Tour of Go: Range and Close](https://go.dev/tour/concurrency/4)
- [Go Spec: For statements with range clause](https://go.dev/ref/spec#For_range)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
