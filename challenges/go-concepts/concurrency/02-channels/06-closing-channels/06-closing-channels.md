---
difficulty: intermediate
concepts: [close, comma-ok, zero-value, broadcast, channel-lifecycle]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [goroutines, unbuffered-channels, buffered-channels, range-channels]
---

# 6. Closing Channels


## Learning Objectives
After completing this exercise, you will be able to:
- **Use** the comma-ok idiom to detect whether a channel is closed
- **Explain** zero-value behavior when reading from a closed channel
- **Implement** broadcasting via `close()` to signal multiple goroutines
- **Avoid** panics from closing or sending on already-closed channels

## Why Understanding Close Semantics

Closing a channel is more than a cleanup step -- it's a communication mechanism. When you close a channel, every receiver gets an immediate signal, even if they weren't waiting yet. This makes `close()` a powerful one-to-many broadcast tool.

But `close()` has sharp edges. Sending on a closed channel panics. Closing an already-closed channel panics. These aren't bugs -- they're invariants that protect you from data corruption. Understanding exactly when reads return the zero value, how the comma-ok idiom works, and when to close (and when not to) separates confident Go programmers from confused ones.

The key mental model: closing a channel is a statement that says "no more values will ever be sent on this channel." It's a permanent, irreversible action.

## Step 1 -- Zero-Value Reads After Close

When a channel is closed, receives immediately return the zero value of the channel's type, forever. The channel never blocks again.

```go
package main

import "fmt"

func main() {
    ch := make(chan int, 3)
    ch <- 10
    ch <- 20
    close(ch)

    // First two reads return buffered values.
    fmt.Printf("Read 1: %d (real value)\n", <-ch)
    fmt.Printf("Read 2: %d (real value)\n", <-ch)
    // After buffer is drained, reads return 0 (int's zero value) forever.
    fmt.Printf("Read 3: %d (zero value -- channel closed and empty)\n", <-ch)
    fmt.Printf("Read 4: %d (zero value -- will repeat forever)\n", <-ch)
}
```

After all buffered values are drained, every subsequent read returns `0` (for int), `""` (for string), `nil` (for pointers), etc.

### Verification
```bash
go run main.go
# Expected:
#   Read 1: 10 (real value)
#   Read 2: 20 (real value)
#   Read 3: 0 (zero value -- channel closed and empty)
#   Read 4: 0 (zero value -- will repeat forever)
```

## Step 2 -- The Comma-Ok Idiom

Use the two-value receive to distinguish "real zero" from "channel closed":

```go
package main

import "fmt"

func main() {
    ch := make(chan int, 2)
    ch <- 0 // intentionally sending zero -- a real value
    close(ch)

    // First read: ok=true -- the zero is a real value sent before close.
    val, ok := <-ch
    fmt.Printf("val=%d, ok=%v  -- real value that happens to be zero\n", val, ok)

    // Second read: ok=false -- the zero is the type's zero value because
    // the channel is closed and empty.
    val, ok = <-ch
    fmt.Printf("val=%d, ok=%v -- zero value because channel is closed\n", val, ok)
}
```

When `ok` is `false`, the channel is closed and drained. When `ok` is `true`, the value is real, even if it happens to be the zero value.

### Verification
```bash
go run main.go
# Expected:
#   val=0, ok=true  -- real value that happens to be zero
#   val=0, ok=false -- zero value because channel is closed
```

## Step 3 -- Broadcasting with Close

Closing a channel unblocks ALL receivers simultaneously. This is the simplest way to broadcast a signal to multiple goroutines. Sending on a channel would only wake ONE receiver.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    quit := make(chan struct{})
    done := make(chan struct{})
    numWorkers := 5

    for i := 0; i < numWorkers; i++ {
        go func(id int) {
            <-quit // blocks until quit is closed
            fmt.Printf("Worker %d: received shutdown signal\n", id)
            done <- struct{}{}
        }(i)
    }

    time.Sleep(50 * time.Millisecond) // let workers start
    fmt.Printf("Broadcasting shutdown to %d workers...\n", numWorkers)
    close(quit) // all 5 workers unblock simultaneously

    for i := 0; i < numWorkers; i++ {
        <-done
    }
}
```

This is the standard pattern for graceful shutdown in Go programs.

### Verification
```bash
go run main.go
# Expected: all 5 workers print their shutdown message
```

What if you used `quit <- struct{}{}` instead of `close(quit)`? Only ONE worker would receive the signal. You'd need to send 5 times for 5 workers. `close()` is the one-to-many broadcast.

## Step 4 -- Panic: Send on Closed Channel

Attempting to send on a closed channel causes an unrecoverable panic.

```go
package main

func main() {
    ch := make(chan int)
    close(ch)
    ch <- 42 // panic: send on closed channel
}
```

### Verification
```bash
go run main.go
# Expected: panic: send on closed channel
```

This is a programming error. The close statement declared "no more sends", then you sent. Go treats this as a violation.

## Step 5 -- Panic: Double Close

Closing an already-closed channel also panics.

```go
package main

func main() {
    ch := make(chan int)
    close(ch)
    close(ch) // panic: close of closed channel
}
```

### Verification
```bash
go run main.go
# Expected: panic: close of closed channel
```

This typically happens when multiple goroutines try to close the same channel without coordination.

## Step 6 -- Graceful Shutdown Dispatcher

A realistic example combining everything: workers process tasks from a buffered channel, main broadcasts shutdown via `close(quit)`, workers finish and signal done.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    tasks := make(chan string, 5)
    quit := make(chan struct{})
    done := make(chan struct{})
    numWorkers := 3

    worker := func(id int) {
        defer func() { done <- struct{}{} }()
        for {
            select {
            case task, ok := <-tasks:
                if !ok {
                    fmt.Printf("Worker %d: task channel closed, exiting\n", id)
                    return
                }
                fmt.Printf("Worker %d: processing %s\n", id, task)
                time.Sleep(30 * time.Millisecond)
            case <-quit:
                fmt.Printf("Worker %d: shutdown signal received\n", id)
                return
            }
        }
    }

    for i := 1; i <= numWorkers; i++ {
        go worker(i)
    }

    for i := 1; i <= 10; i++ {
        tasks <- fmt.Sprintf("task-%d", i)
    }

    time.Sleep(150 * time.Millisecond) // let workers process some tasks

    fmt.Println("Sending shutdown signal...")
    close(quit) // broadcast to all workers

    for i := 0; i < numWorkers; i++ {
        <-done
    }
    fmt.Println("All workers shut down cleanly")
}
```

### Verification
```bash
go run main.go
# Expected: workers process some tasks, then all receive shutdown and exit
```

## Common Mistakes

### Using Close as "I'm Done Receiving"

**Wrong:**
```go
// Consumer code:
val := <-ch
close(ch) // "I'm done reading"
```

**What happens:** If the producer sends another value, it panics.

**Fix:** Only the sender closes the channel. The receiver just stops reading. If you need to signal the producer to stop, use a separate "done" or "quit" channel.

### No Built-In isOpen() Check

**Wrong approach:**
```go
if isOpen(ch) {
    ch <- value // race: channel might close between check and send
}
```

**What happens:** There is no `isOpen()` function in Go, and even if you check with comma-ok, the state can change between the check and your next operation.

**Fix:** Structure your code so that ownership is clear. The owner (sender) is the only one who closes. Use `select` for non-blocking operations.

## What's Next
Continue to [07-nil-channel-behavior](../07-nil-channel-behavior/07-nil-channel-behavior.md) to learn the surprising behavior of nil channels and how to use them strategically.

## Summary
- Closed channels return zero values on receive, immediately and forever
- `val, ok := <-ch` -- when `ok` is `false`, the channel is closed and empty
- `close(ch)` unblocks ALL waiting receivers simultaneously (broadcast)
- Sending on a closed channel panics -- only the sender should close
- Closing an already-closed channel panics -- coordinate who closes
- Close communicates "no more values" -- it's a permanent declaration

## Reference
- [Go Spec: Close](https://go.dev/ref/spec#Close)
- [Go Spec: Receive operator](https://go.dev/ref/spec#Receive_operator)
- [Go FAQ: How do I know if a channel is closed?](https://go.dev/doc/faq#closechan)
