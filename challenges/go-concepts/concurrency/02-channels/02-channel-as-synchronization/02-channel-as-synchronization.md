# 2. Channel as Synchronization

<!--
difficulty: basic
concepts: [channels, synchronization, done-channel, signaling, goroutine-coordination]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, unbuffered-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (unbuffered channel basics)
- Understanding of goroutine lifecycle

## Learning Objectives
After completing this exercise, you will be able to:
- **Replace** fragile `time.Sleep` synchronization with channel-based signaling
- **Implement** the done-channel pattern to wait for goroutine completion
- **Explain** why channel synchronization is deterministic while sleep is not

## Why Channel Synchronization

When you first learn goroutines, `time.Sleep` seems like a quick way to "wait" for goroutines to finish. But `time.Sleep` is a guess -- you're betting that the goroutine finishes within the sleep duration. On a slow machine, under heavy load, or with network calls, that bet fails silently. Your program exits before the goroutine finishes, and you lose results with no error message.

Channels give you a guarantee instead of a guess. When you receive from a done channel, you know the goroutine has completed because it sent the signal. It doesn't matter if the work took 1ms or 10 seconds -- the receiver waits exactly as long as needed.

This pattern is so fundamental that it appears in virtually every production Go program. It's the building block for more sophisticated patterns like fan-out/fan-in, pipelines, and graceful shutdown.

## Step 1 -- The Fragile Sleep Version

Start with code that uses `time.Sleep` to wait for goroutines. Observe how it breaks when the work takes longer than expected.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    worker := func(id int) {
        fmt.Printf("Worker %d: starting\n", id)
        time.Sleep(time.Duration(id*100) * time.Millisecond)
        fmt.Printf("Worker %d: done\n", id)
    }

    for i := 1; i <= 3; i++ {
        go worker(i)
    }

    // Hope that 200ms is enough... it's not for worker 3 (needs 300ms)
    time.Sleep(200 * time.Millisecond)
    fmt.Println("main: exiting (worker 3 lost!)")
}
```

Worker 3 needs 300ms but main only waits 200ms. Worker 3's completion message is lost.

### Verification
```bash
go run main.go
# You'll see worker 3 is missing its "done" message
```

## Step 2 -- Convert to Done Channel

Replace `time.Sleep` with a done channel. Each goroutine signals completion by sending on the channel. Main receives once per worker, guaranteeing all finish.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    done := make(chan bool)

    worker := func(id int) {
        fmt.Printf("Worker %d: starting\n", id)
        time.Sleep(time.Duration(id*100) * time.Millisecond)
        fmt.Printf("Worker %d: done\n", id)
        done <- true // signal completion
    }

    for i := 1; i <= 3; i++ {
        go worker(i)
    }

    // Receive once per worker. Blocks until ALL three have sent.
    // It doesn't matter if a worker takes 1ms or 10s -- we wait exactly as needed.
    for i := 0; i < 3; i++ {
        <-done
    }
    fmt.Println("main: all workers completed")
}
```

### Verification
```bash
go run main.go
# Expected: all three workers print "done" messages, then main exits
# Worker 1: starting
# Worker 2: starting
# Worker 3: starting
# Worker 1: done
# Worker 2: done
# Worker 3: done
# main: all workers completed
```

## Step 3 -- Signal Without Data: struct{}

When a channel is used purely for signaling (the value itself doesn't matter), use `chan struct{}` instead of `chan bool`. It communicates intent clearly and uses zero memory per value.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    done := make(chan struct{})

    for i := 1; i <= 3; i++ {
        go func(id int) {
            time.Sleep(time.Duration(id*50) * time.Millisecond)
            fmt.Printf("Worker %d: finished task\n", id)
            // struct{}{} carries no data -- the synchronization IS the message.
            done <- struct{}{}
        }(i)
    }

    for i := 0; i < 3; i++ {
        <-done
    }
    fmt.Println("all 3 workers confirmed done")
}
```

Why `struct{}` over `bool`? Three reasons:
1. **Intent**: `chan struct{}` says "this is purely a signal" at the type level
2. **Memory**: `struct{}` is zero bytes; `bool` is one byte (negligible, but principled)
3. **Convention**: idiomatic Go uses `chan struct{}` for done/quit channels

### Verification
```bash
go run main.go
# Same behavior as step 2, but with clearer intent
```

## Step 4 -- Collecting Results (Not Just Signals)

In practice, goroutines often produce results. The channel carries both the data AND the synchronization in one operation.

```go
package main

import (
    "fmt"
    "time"
)

type Result struct {
    WorkerID int
    Data     string
}

func main() {
    results := make(chan Result)

    for i := 1; i <= 3; i++ {
        go func(id int) {
            time.Sleep(time.Duration(id*50) * time.Millisecond)
            results <- Result{
                WorkerID: id,
                Data:     fmt.Sprintf("data-from-%d", id),
            }
        }(i)
    }

    for i := 0; i < 3; i++ {
        r := <-results
        fmt.Printf("Worker %d result: %s\n", r.WorkerID, r.Data)
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   Worker 1 result: data-from-1
#   Worker 2 result: data-from-2
#   Worker 3 result: data-from-3
```

## Step 5 -- Total Time Equals Slowest Worker

The power of concurrent synchronization: total elapsed time equals the slowest worker, not the sum of all workers. This program proves it.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    start := time.Now()
    done := make(chan struct{})
    numTasks := 5

    for i := 1; i <= numTasks; i++ {
        go func(id int) {
            duration := time.Duration(id*200) * time.Millisecond
            fmt.Printf("Task %d: working for %v\n", id, duration)
            time.Sleep(duration)
            fmt.Printf("Task %d: complete\n", id)
            done <- struct{}{}
        }(i)
    }

    for i := 0; i < numTasks; i++ {
        <-done
    }

    elapsed := time.Since(start).Round(100 * time.Millisecond)
    fmt.Printf("Total time: %v (parallel -- not the sum)\n", elapsed)
}
```

### Verification
```bash
go run main.go
# Expected: Total time ~1s (slowest task), NOT ~3s (sum of all)
```

If you added `time.Sleep` instead of channels, you'd have to guess the maximum. With channels, you wait exactly as long as the slowest worker -- no more, no less.

## Common Mistakes

### Mismatched Send/Receive Count

**Wrong:**
```go
package main

import "fmt"

func main() {
    done := make(chan struct{})
    for i := 0; i < 5; i++ {
        go func() { done <- struct{}{} }()
    }
    for i := 0; i < 3; i++ { // only waiting for 3!
        <-done
    }
    fmt.Println("done") // 2 goroutines still running (or trying to send)
}
```

**What happens:** Main exits while 2 goroutines are still running. You lose work.

**Correct:** Always match the number of receives to the number of goroutines launched:
```go
for i := 0; i < 5; i++ {
    <-done // receive 5 times for 5 goroutines
}
```

### Sending Before the Work Is Done

**Wrong:**
```go
package main

import (
    "fmt"
    "time"
)

func main() {
    done := make(chan struct{})
    go func() {
        done <- struct{}{} // signal sent BEFORE work!
        time.Sleep(1 * time.Second) // "expensive work"
        fmt.Println("work complete")
    }()
    <-done
    fmt.Println("main continues") // goroutine is still working!
}
```

**What happens:** The signal arrives before the work completes. Main proceeds with incomplete results.

**Correct:** Always send the done signal as the LAST operation in the goroutine.

## What's Next
Continue to [03-buffered-channels](../03-buffered-channels/03-buffered-channels.md) to learn how buffered channels decouple senders from receivers.

## Summary
- `time.Sleep` for synchronization is fragile -- it guesses instead of guaranteeing
- Done channels provide deterministic synchronization: receive blocks until the sender signals
- Use `chan struct{}` for pure signaling where the value doesn't matter
- To wait for N goroutines, perform N receives on the done channel
- Always send the done signal as the last operation in the goroutine
- Result channels combine synchronization with data transfer

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/concurrency-patterns)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
