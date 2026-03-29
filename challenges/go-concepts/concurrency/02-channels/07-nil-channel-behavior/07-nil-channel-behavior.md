---
difficulty: intermediate
concepts: [nil-channel, select, dynamic-disable, channel-state-machine]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, unbuffered-channels, buffered-channels, close, select-basics]
---

# 7. Nil Channel Behavior


## Learning Objectives
After completing this exercise, you will be able to:
- **Predict** the behavior of nil channels (block forever on send and receive)
- **Use** nil channels in `select` to dynamically disable cases
- **Implement** the "set to nil after close" pattern for merging multiple channels
- **Distinguish** between nil, open, and closed channel behavior

## Why Nil Channels

At first, nil channels seem like a bug -- they block forever on both send and receive. Why would you ever want that? The answer lies in `select`. When a channel is nil, its `select` case is never chosen. This lets you dynamically enable and disable cases at runtime.

Consider merging two channels: you read from both until both are closed. Without nil channels, you'd need complex boolean flags. With nil channels, when one source closes, you set its variable to nil. The `select` naturally stops considering that case. The code is cleaner, shorter, and harder to get wrong.

This pattern appears in production code for merging event streams, implementing timeouts that can be canceled, and building state machines where available operations change over time.

## Channel State Summary

| State | Send | Receive | Close |
|-------|------|---------|-------|
| nil | Block forever | Block forever | panic |
| open, empty | Block (if unbuffered or full) | Block | OK |
| open, has data | Send or block | Receive value | OK |
| closed | panic | Zero value (ok=false) | panic |

## Step 1 -- Nil Channel Blocks Forever

Demonstrate that a nil channel blocks on both send and receive.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    var ch chan int // nil -- not initialized with make()

    // Prove receive blocks by racing against a timeout.
    select {
    case val := <-ch:
        fmt.Println("received:", val) // never happens
    case <-time.After(200 * time.Millisecond):
        fmt.Println("receive on nil: timed out (as expected)")
    }

    // Prove send blocks the same way.
    select {
    case ch <- 42:
        fmt.Println("sent") // never happens
    case <-time.After(200 * time.Millisecond):
        fmt.Println("send on nil: timed out (as expected)")
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   receive on nil: timed out (as expected)
#   send on nil: timed out (as expected)
```

Without the `select` + timeout, `<-ch` on a nil channel would deadlock (or block the goroutine forever if other goroutines exist).

## Step 2 -- Nil Channel in Select Is Skipped

When a channel variable is nil, its `select` case is permanently skipped -- as if it doesn't exist.

```go
package main

import "fmt"

func main() {
    var disabled chan int // nil -- this case will be skipped
    backup := make(chan int, 1)
    backup <- 99

    select {
    case val := <-disabled:
        fmt.Println("disabled:", val) // never chosen -- disabled is nil
    case val := <-backup:
        fmt.Println("backup channel selected:", val) // always chosen
    }
}
```

This is the key insight that makes nil channels useful. You can dynamically control which select cases are active by assigning channel variables to nil or to a real channel.

### Verification
```bash
go run main.go
# Expected: backup channel selected: 99
```

## Step 3 -- Merge Two Channels with Nil Disabling

The core pattern: merge values from two channels until both are closed. When one closes, set it to nil so `select` stops trying to read from it.

```go
package main

import "fmt"

func merge(a, b <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        // Loop while at least one channel is still open (non-nil).
        for a != nil || b != nil {
            select {
            case val, ok := <-a:
                if !ok {
                    a = nil // disable this case
                    continue
                }
                out <- val
            case val, ok := <-b:
                if !ok {
                    b = nil // disable this case
                    continue
                }
                out <- val
            }
        }
    }()
    return out
}

func main() {
    evens := make(chan int)
    odds := make(chan int)

    go func() {
        for _, v := range []int{2, 4, 6} {
            evens <- v
        }
        close(evens)
    }()

    go func() {
        for _, v := range []int{1, 3, 5, 7} {
            odds <- v
        }
        close(odds)
    }()

    count := 0
    for val := range merge(evens, odds) {
        fmt.Println(val)
        count++
    }
    fmt.Printf("Merge complete: received %d values\n", count)
}
```

When `a` is closed, we set `a = nil`. The next iteration still enters `select`, but `case <-a` is skipped because `a` is nil. Only `case <-b` is considered. When both are nil, the loop exits.

### Verification
```bash
go run main.go
# Expected: all 7 values from both channels (order may vary), then "Merge complete: received 7 values"
```

## Step 4 -- Dynamic Enable/Disable in a State Machine

Use nil channels to model a worker with pause/resume capabilities. When paused, the jobs channel variable is set to nil, disabling job processing. When resumed, it's restored.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    jobs := make(chan string, 10)
    pauseCh := make(chan struct{})
    resumeCh := make(chan struct{})
    done := make(chan struct{})

    for i := 1; i <= 8; i++ {
        jobs <- fmt.Sprintf("job-%d", i)
    }

    go func() {
        active := jobs // start in active state
        for {
            select {
            case job, ok := <-active:
                if !ok {
                    fmt.Println("Worker: jobs channel closed, exiting")
                    done <- struct{}{}
                    return
                }
                fmt.Println("Worker: processing", job)
                time.Sleep(40 * time.Millisecond)
            case <-pauseCh:
                fmt.Println("Worker: PAUSED")
                active = nil // nil disables the job case
            case <-resumeCh:
                fmt.Println("Worker: RESUMED")
                active = jobs // restore to re-enable
            }
        }
    }()

    time.Sleep(150 * time.Millisecond)
    pauseCh <- struct{}{}
    fmt.Println("Main: pause sent")

    time.Sleep(200 * time.Millisecond)
    resumeCh <- struct{}{}
    fmt.Println("Main: resume sent")

    time.Sleep(200 * time.Millisecond)
    close(jobs)
    <-done
}
```

### Verification
```bash
go run main.go
# Expected: worker processes some jobs, pauses (stops processing), resumes, processes remaining, exits
```

## Step 5 -- Priority Merger

A practical application: merging event streams with different priority levels.

```go
package main

import (
    "fmt"
    "time"
)

func priorityMerge(high, low <-chan string) <-chan string {
    out := make(chan string)
    go func() {
        defer close(out)
        for high != nil || low != nil {
            select {
            case msg, ok := <-high:
                if !ok {
                    high = nil
                    continue
                }
                out <- "[HIGH] " + msg
            case msg, ok := <-low:
                if !ok {
                    low = nil
                    continue
                }
                out <- "[LOW] " + msg
            }
        }
    }()
    return out
}

func main() {
    high := make(chan string)
    low := make(chan string)

    go func() {
        for _, msg := range []string{"alert", "critical", "urgent"} {
            high <- msg
            time.Sleep(30 * time.Millisecond)
        }
        close(high)
    }()

    go func() {
        for _, msg := range []string{"info-1", "info-2", "info-3", "info-4", "info-5"} {
            low <- msg
            time.Sleep(20 * time.Millisecond)
        }
        close(low)
    }()

    for msg := range priorityMerge(high, low) {
        fmt.Println(msg)
    }
}
```

### Verification
```bash
go run main.go
# Expected: all 8 messages with [HIGH] or [LOW] prefix, high-priority source eventually closes, only low messages remain
```

## Common Mistakes

### Forgetting That var Declares a Nil Channel

**Wrong:**
```go
package main

func main() {
    var results chan int
    go func() {
        results <- 42 // blocks forever -- results is nil!
    }()
    <-results // also blocks forever
}
```

**What happens:** Both goroutines block permanently on the nil channel. Deadlock.

**Fix:** Always use `make(chan int)` to create a usable channel.

### Not Checking Both Channels Are Nil Before Exiting

**Wrong:**
```go
for {
    select {
    case val, ok := <-a:
        if !ok { return } // exits when a closes, losing remaining b values!
    case val, ok := <-b:
        if !ok { return }
    }
}
```

**What happens:** When `a` closes, you return immediately, losing all remaining values in `b`.

**Correct:** Set to nil instead of returning. Only exit when both are nil:
```go
for a != nil || b != nil {
    select {
    case val, ok := <-a:
        if !ok { a = nil; continue }
        process(val)
    case val, ok := <-b:
        if !ok { b = nil; continue }
        process(val)
    }
}
```

## What's Next
Continue to [08-channel-of-channels](../08-channel-of-channels/08-channel-of-channels.md) to learn how to pass channels through channels for request-response patterns.

## Summary
- A nil channel blocks forever on both send and receive
- In `select`, a nil channel's case is never chosen (effectively disabled)
- Set a channel to nil after it closes to stop `select` from considering it
- Pattern for merging N channels: loop while any channel is non-nil, set to nil as each closes
- This enables dynamic state machines where available operations change at runtime
- Always initialize channels with `make()` unless you intentionally want nil behavior

## Reference
- [Go Spec: Channel types (nil behavior)](https://go.dev/ref/spec#Channel_types)
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Dave Cheney: Channel Axioms](https://dave.cheney.net/2014/03/19/channel-axioms)
