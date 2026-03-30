---
difficulty: intermediate
concepts: [close, comma-ok, zero-value, broadcast, channel-lifecycle]
tools: [go]
estimated_time: 25m
bloom_level: analyze
---

# 6. Closing Channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** the comma-ok idiom to detect whether a channel is closed
- **Explain** zero-value behavior when reading from a closed channel
- **Implement** broadcasting via `close()` to signal multiple workers
- **Avoid** panics from closing or sending on already-closed channels

## Why Understanding Close Semantics

Imagine a task dispatcher that distributes work items to a pool of workers. When there is no more work, the dispatcher needs to tell ALL workers to shut down. You could send a "stop" message to each worker individually, but that requires knowing exactly how many workers exist and ensuring each gets exactly one stop message.

`close()` solves this elegantly: closing a channel unblocks ALL receivers simultaneously. Every worker blocking on `<-tasks` gets an immediate zero-value response. This one-to-many broadcast is the standard way to signal "no more work" in Go.

But `close()` has sharp edges. Sending on a closed channel panics. Closing an already-closed channel panics. These are not bugs -- they are invariants that protect you from data corruption. Understanding when reads return the zero value, how the comma-ok idiom distinguishes "real zero" from "closed channel," and when to close (and when not to) separates confident Go programmers from confused ones.

## Step 1 -- Zero-Value Reads After Close

When a task channel is closed, receives immediately return the zero value of the channel's type, forever. Workers must be able to detect this to know when to stop.

```go
package main

import "fmt"

type Task struct {
    ID   int
    Name string
}

func main() {
    tasks := make(chan Task, 3)
    tasks <- Task{ID: 1, Name: "process-invoice"}
    tasks <- Task{ID: 2, Name: "send-email"}
    close(tasks)

    // First two reads return real tasks.
    fmt.Printf("Read 1: %+v (real task)\n", <-tasks)
    fmt.Printf("Read 2: %+v (real task)\n", <-tasks)
    // After buffer is drained, reads return zero value forever.
    fmt.Printf("Read 3: %+v (zero value -- channel closed and empty)\n", <-tasks)
    fmt.Printf("Read 4: %+v (zero value -- repeats forever)\n", <-tasks)
}
```

After all buffered tasks are drained, every subsequent read returns `Task{ID: 0, Name: ""}`. For int channels, you get `0`. For string channels, `""`. For pointers, `nil`.

### Verification
```bash
go run main.go
# Expected:
#   Read 1: {ID:1 Name:process-invoice} (real task)
#   Read 2: {ID:2 Name:send-email} (real task)
#   Read 3: {ID:0 Name:} (zero value -- channel closed and empty)
#   Read 4: {ID:0 Name:} (zero value -- repeats forever)
```

## Step 2 -- The Comma-Ok Idiom: Distinguishing Real Data from Shutdown

A task with ID=0 could be a legitimate task or a zero value from a closed channel. The comma-ok idiom resolves this ambiguity.

```go
package main

import "fmt"

func main() {
    tasks := make(chan int, 2)
    tasks <- 0 // intentionally sending zero -- this is a real task ID
    close(tasks)

    // First read: ok=true -- the zero is a real task ID sent before close.
    id, ok := <-tasks
    fmt.Printf("id=%d, ok=%v -- real task (happens to have ID 0)\n", id, ok)

    // Second read: ok=false -- the zero means the channel is closed and empty.
    id, ok = <-tasks
    fmt.Printf("id=%d, ok=%v -- channel closed, no more tasks\n", id, ok)
}
```

When `ok` is `false`, the channel is closed and drained -- the worker should stop. When `ok` is `true`, the value is a real task, even if it happens to be the zero value.

### Verification
```bash
go run main.go
# Expected:
#   id=0, ok=true -- real task (happens to have ID 0)
#   id=0, ok=false -- channel closed, no more tasks
```

## Step 3 -- Broadcasting Shutdown to Multiple Workers

Closing a channel unblocks ALL receivers simultaneously. This is the simplest way to tell a pool of workers "no more work." Sending on a channel would only wake ONE worker.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    shutdown := make(chan struct{})
    done := make(chan struct{})
    numWorkers := 5

    for i := 1; i <= numWorkers; i++ {
        go func(id int) {
            fmt.Printf("Worker %d: waiting for tasks...\n", id)
            <-shutdown // blocks until shutdown is closed
            fmt.Printf("Worker %d: received shutdown broadcast, cleaning up\n", id)
            done <- struct{}{}
        }(i)
    }

    time.Sleep(50 * time.Millisecond) // let workers start
    fmt.Printf("\nDispatcher: no more work -- broadcasting shutdown to %d workers...\n\n", numWorkers)
    close(shutdown) // ALL 5 workers unblock simultaneously

    for i := 0; i < numWorkers; i++ {
        <-done
    }
    fmt.Println("Dispatcher: all workers shut down cleanly")
}
```

What if you used `shutdown <- struct{}{}` instead of `close(shutdown)`? Only ONE worker would receive the signal. You would need to send 5 times for 5 workers, and you would need to know exactly how many workers exist. `close()` is the one-to-many broadcast.

### Verification
```bash
go run main.go
# Expected: all 5 workers print their shutdown message
```

## Step 4 -- Panic: Send on Closed Channel and Double Close

These are the two sharp edges of close. Both result in unrecoverable panics.

```go
package main

import "fmt"

func main() {
    // Demonstrate send-on-closed-channel panic.
    func() {
        defer func() {
            if r := recover(); r != nil {
                fmt.Printf("Caught panic: %v\n", r)
            }
        }()
        tasks := make(chan int)
        close(tasks)
        tasks <- 42 // panic: send on closed channel
    }()

    // Demonstrate double-close panic.
    func() {
        defer func() {
            if r := recover(); r != nil {
                fmt.Printf("Caught panic: %v\n", r)
            }
        }()
        tasks := make(chan int)
        close(tasks)
        close(tasks) // panic: close of closed channel
    }()

    fmt.Println("Both panics caught and handled")
}
```

### Verification
```bash
go run main.go
# Expected:
#   Caught panic: send on closed channel
#   Caught panic: close of closed channel
#   Both panics caught and handled
```

## Step 5 -- Task Dispatcher with Graceful Shutdown

A realistic example combining everything: the dispatcher sends work items to workers through a buffered task channel. Workers use the comma-ok idiom to distinguish real tasks from shutdown. When work is done, the dispatcher closes the task channel, and all workers exit cleanly.

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
                fmt.Printf("Worker %d: emergency shutdown\n", id)
                return
            }
        }
    }

    for i := 1; i <= numWorkers; i++ {
        go worker(i)
    }

    // Dispatcher sends 10 tasks.
    for i := 1; i <= 10; i++ {
        tasks <- fmt.Sprintf("task-%d", i)
    }

    // Signal no more work by closing the task channel.
    close(tasks)

    for i := 0; i < numWorkers; i++ {
        <-done
    }
    fmt.Println("Dispatcher: all workers finished")
    _ = quit // quit channel available for emergency shutdown scenarios
}
```

### Verification
```bash
go run main.go
# Expected: workers process all 10 tasks, then each detects the closed channel and exits
```

## Intermediate Verification

Run the programs and confirm:
1. Zero values are returned from closed channels indefinitely
2. The comma-ok idiom correctly distinguishes real data from closed-channel zero values
3. `close()` broadcasts to all blocked receivers simultaneously
4. Sending on a closed channel and double-closing both panic

## Common Mistakes

### Using Close as "I'm Done Receiving"

**Wrong:**
```go
// Consumer code:
task := <-tasks
close(tasks) // "I'm done reading"
```

**What happens:** If the dispatcher sends another task, it panics.

**Fix:** Only the sender closes the channel. The receiver just stops reading. If you need to signal the producer to stop, use a separate "quit" channel.

### No Built-In isOpen() Check

**Wrong approach:**
```go
if isOpen(ch) {
    ch <- value // race: channel might close between check and send
}
```

**What happens:** There is no `isOpen()` function in Go, and even if you check with comma-ok, the state can change between the check and your next operation.

**Fix:** Structure your code so that ownership is clear. The owner (sender) is the only one who closes. Use `select` for non-blocking operations.

## Verify What You Learned
1. What does `val, ok := <-ch` return when the channel is closed and empty?
2. Why does closing a channel broadcast to ALL receivers instead of just one?
3. Why is it a programming error to send on a closed channel?

## What's Next
Continue to [07-nil-channel-behavior](../07-nil-channel-behavior/07-nil-channel-behavior.md) to learn the surprising behavior of nil channels and how to use them strategically.

## Summary
- Closed channels return zero values on receive, immediately and forever
- `val, ok := <-ch` -- when `ok` is `false`, the channel is closed and empty
- `close(ch)` unblocks ALL waiting receivers simultaneously (broadcast)
- Sending on a closed channel panics -- only the sender should close
- Closing an already-closed channel panics -- coordinate who closes
- Close communicates "no more values" -- it is a permanent, irreversible declaration

## Reference
- [Go Spec: Close](https://go.dev/ref/spec#Close)
- [Go Spec: Receive operator](https://go.dev/ref/spec#Receive_operator)
- [Go FAQ: How do I know if a channel is closed?](https://go.dev/doc/faq#closechan)
