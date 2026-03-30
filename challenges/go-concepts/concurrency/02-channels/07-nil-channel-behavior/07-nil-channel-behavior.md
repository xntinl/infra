---
difficulty: intermediate
concepts: [nil-channel, select, dynamic-disable, channel-state-machine]
tools: [go]
estimated_time: 30m
bloom_level: analyze
---

# 7. Nil Channel Behavior

## Learning Objectives
After completing this exercise, you will be able to:
- **Predict** the behavior of nil channels (block forever on send and receive)
- **Use** nil channels in `select` to dynamically disable cases
- **Implement** the "set to nil after close" pattern for merging multiple channels
- **Distinguish** between nil, open, and closed channel behavior

## Why Nil Channels

Consider a system that merges sorted data from multiple sources -- a database query stream and a cache stream, for example. Each stream finishes at a different time. When the cache stream is exhausted, you need to stop reading from it but continue reading from the database stream until it also finishes.

Without nil channels, you would need complex boolean flags, nested if-statements, and careful coordination. With nil channels, when one source closes, you set its variable to nil. The `select` naturally stops considering that case. The code is cleaner, shorter, and harder to get wrong.

This pattern appears in production code for merging event streams, implementing timeouts that can be canceled, and building state machines where available operations change over time.

## Channel State Summary

| State | Send | Receive | Close |
|-------|------|---------|-------|
| nil | Block forever | Block forever | panic |
| open, empty | Block (if unbuffered or full) | Block | OK |
| open, has data | Send or block | Receive value | OK |
| closed | panic | Zero value (ok=false) | panic |

## Step 1 -- Nil Channel Blocks Forever

Demonstrate that a nil channel blocks on both send and receive. This is not a bug -- it is a property we will exploit in `select`.

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
        fmt.Println("receive on nil channel: blocked (timed out as expected)")
    }

    // Prove send blocks the same way.
    select {
    case ch <- 42:
        fmt.Println("sent") // never happens
    case <-time.After(200 * time.Millisecond):
        fmt.Println("send on nil channel: blocked (timed out as expected)")
    }
}
```

### Verification
```bash
go run main.go
# Expected:
#   receive on nil channel: blocked (timed out as expected)
#   send on nil channel: blocked (timed out as expected)
```

Without the `select` + timeout, `<-ch` on a nil channel would deadlock (or block the goroutine forever if other goroutines exist).

## Step 2 -- Nil Channel in Select Is Skipped

When a channel variable is nil, its `select` case is never chosen -- as if it does not exist. This is the key insight that makes nil channels useful.

```go
package main

import "fmt"

func main() {
    var dbStream chan string    // nil -- this case will be skipped
    cacheStream := make(chan string, 1)
    cacheStream <- "user:42:cached"

    select {
    case val := <-dbStream:
        fmt.Println("from database:", val) // never chosen -- dbStream is nil
    case val := <-cacheStream:
        fmt.Println("from cache:", val) // always chosen
    }
}
```

You can dynamically control which select cases are active by assigning channel variables to nil or to a real channel.

### Verification
```bash
go run main.go
# Expected: from cache: user:42:cached
```

## Step 3 -- Merging Sorted Streams

The core pattern: merge values from two sorted streams until both are exhausted. When one stream closes, set it to nil so `select` stops trying to read from it. The other stream continues until it also closes.

```go
package main

import "fmt"

func sortedStream(values []int) <-chan int {
    ch := make(chan int)
    go func() {
        for _, v := range values {
            ch <- v
        }
        close(ch)
    }()
    return ch
}

func mergeSorted(a, b <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        // Loop while at least one stream is still open (non-nil).
        for a != nil || b != nil {
            select {
            case val, ok := <-a:
                if !ok {
                    fmt.Println("  [merge] stream A exhausted -- disabling")
                    a = nil // disable this case in select
                    continue
                }
                out <- val
            case val, ok := <-b:
                if !ok {
                    fmt.Println("  [merge] stream B exhausted -- disabling")
                    b = nil // disable this case in select
                    continue
                }
                out <- val
            }
        }
    }()
    return out
}

func main() {
    // Two sorted streams of different lengths.
    streamA := sortedStream([]int{10, 30, 50})
    streamB := sortedStream([]int{20, 40, 60, 80, 100})

    fmt.Println("Merged output:")
    count := 0
    for val := range mergeSorted(streamA, streamB) {
        fmt.Printf("  %d\n", val)
        count++
    }
    fmt.Printf("Merge complete: %d values from both streams\n", count)
}
```

When `a` is closed, we set `a = nil`. The next iteration still enters `select`, but `case <-a` is skipped because `a` is nil. Only `case <-b` is considered. When both are nil, the loop exits.

### Verification
```bash
go run main.go
# Expected: all 8 values from both streams (interleaved order may vary),
# then "Merge complete: 8 values from both streams"
```

## Step 4 -- Merging Three Data Sources with Dynamic Disabling

Extend the pattern to merge three sources -- a scenario you encounter when aggregating data from multiple microservices. Each service responds at a different speed and produces a different number of results.

```go
package main

import (
    "fmt"
    "time"
)

type Event struct {
    Source string
    Data   string
}

func eventStream(source string, events []string, delay time.Duration) <-chan Event {
    ch := make(chan Event)
    go func() {
        for _, e := range events {
            time.Sleep(delay)
            ch <- Event{Source: source, Data: e}
        }
        close(ch)
    }()
    return ch
}

func mergeEvents(sources ...<-chan Event) <-chan Event {
    out := make(chan Event)
    go func() {
        defer close(out)
        active := make([]<-chan Event, len(sources))
        copy(active, sources)

        for {
            allNil := true
            for _, ch := range active {
                if ch != nil {
                    allNil = false
                    break
                }
            }
            if allNil {
                return
            }

            // Build select dynamically using reflect would be complex.
            // For 3 known sources, explicit select is clearer.
            select {
            case ev, ok := <-active[0]:
                if !ok {
                    active[0] = nil
                    continue
                }
                out <- ev
            case ev, ok := <-active[1]:
                if !ok {
                    active[1] = nil
                    continue
                }
                out <- ev
            case ev, ok := <-active[2]:
                if !ok {
                    active[2] = nil
                    continue
                }
                out <- ev
            }
        }
    }()
    return out
}

func main() {
    // Three services with different speeds and data volumes.
    userSvc := eventStream("users", []string{"user-created", "user-updated"}, 30*time.Millisecond)
    orderSvc := eventStream("orders", []string{"order-placed", "order-shipped", "order-delivered"}, 20*time.Millisecond)
    paymentSvc := eventStream("payments", []string{"payment-received"}, 50*time.Millisecond)

    fmt.Println("Aggregating events from 3 services:")
    for ev := range mergeEvents(userSvc, orderSvc, paymentSvc) {
        fmt.Printf("  [%-8s] %s\n", ev.Source, ev.Data)
    }
    fmt.Println("All services exhausted")
}
```

### Verification
```bash
go run main.go
# Expected: all 6 events from 3 services, interleaved by timing, then "All services exhausted"
```

## Step 5 -- Dynamic Enable/Disable: Pausable Worker

Use nil channels to model a worker with pause/resume capabilities. When paused, the task channel variable is set to nil, disabling task processing in the select. When resumed, it is restored.

```go
package main

import (
    "fmt"
    "time"
)

func main() {
    tasks := make(chan string, 10)
    pauseCh := make(chan struct{})
    resumeCh := make(chan struct{})
    done := make(chan struct{})

    for i := 1; i <= 8; i++ {
        tasks <- fmt.Sprintf("deploy-service-%d", i)
    }

    go func() {
        active := tasks // start in active state
        for {
            select {
            case task, ok := <-active:
                if !ok {
                    fmt.Println("Worker: all tasks complete, exiting")
                    done <- struct{}{}
                    return
                }
                fmt.Println("Worker: executing", task)
                time.Sleep(40 * time.Millisecond)
            case <-pauseCh:
                fmt.Println("Worker: PAUSED (maintenance window)")
                active = nil // nil disables the task case in select
            case <-resumeCh:
                fmt.Println("Worker: RESUMED")
                active = tasks // restore to re-enable
            }
        }
    }()

    time.Sleep(150 * time.Millisecond) // let worker process some tasks
    pauseCh <- struct{}{}
    fmt.Println("Main: pause sent -- simulating maintenance window")

    time.Sleep(200 * time.Millisecond) // maintenance period
    resumeCh <- struct{}{}
    fmt.Println("Main: resume sent -- maintenance complete")

    time.Sleep(250 * time.Millisecond)
    close(tasks)
    <-done
}
```

### Verification
```bash
go run main.go
# Expected: worker processes some tasks, pauses, resumes, processes remaining, exits
```

## Intermediate Verification

Run the programs and confirm:
1. Nil channels block forever on send and receive
2. Nil channels are skipped in select statements
3. Setting a channel to nil after close prevents select from spinning on zero values
4. The merge pattern correctly handles sources that close at different times

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

### Not Checking All Channels Are Nil Before Exiting

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

## Verify What You Learned
1. What happens when you read from a nil channel outside of a select?
2. Why is setting a closed channel to nil better than just checking `ok` each time?
3. How would you merge 10 channels using the nil pattern without writing 10 select cases?

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
