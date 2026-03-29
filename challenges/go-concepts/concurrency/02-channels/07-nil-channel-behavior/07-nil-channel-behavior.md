# 7. Nil Channel Behavior

<!--
difficulty: intermediate
concepts: [nil-channel, select, dynamic-disable, channel-state-machine]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, unbuffered-channels, buffered-channels, close, select-basics]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-06 (channels through closing)
- Basic familiarity with `select` (reading from multiple channels)

## Learning Objectives
After completing this exercise, you will be able to:
- **Predict** the behavior of nil channels (block forever on send and receive)
- **Use** nil channels in `select` to dynamically disable cases
- **Implement** the "set to nil after close" pattern for merging multiple channels
- **Distinguish** between nil, open, and closed channel behavior

## Why Nil Channels

At first, nil channels seem like a bug — they block forever on both send and receive. Why would you ever want that? The answer lies in `select`. When a channel is nil, its `select` case is never chosen. This lets you dynamically enable and disable cases at runtime.

Consider merging two channels: you read from both until both are closed. Without nil channels, you'd need complex boolean flags. With nil channels, when one source closes, you set its variable to nil. The `select` naturally stops considering that case. The code is cleaner, shorter, and harder to get wrong.

This pattern appears in production code for merging event streams, implementing timeouts that can be canceled, and building state machines where available operations change over time. It's one of Go's most elegant concurrency idioms.

## Step 1 -- Nil Channel Blocks Forever

Demonstrate that a nil channel blocks on both send and receive.

```go
var ch chan int // nil — not initialized with make()

// This would block forever:
// ch <- 42    // blocks
// val := <-ch // blocks

// But Go's deadlock detector catches it if no other goroutine exists:
// go run with just <-ch will deadlock
```

Prove it with a timeout:
```go
var ch chan int

select {
case val := <-ch:
    fmt.Println("received:", val) // never happens
case <-time.After(1 * time.Second):
    fmt.Println("nil channel: receive timed out (as expected)")
}
```

### Intermediate Verification
```bash
go run main.go
# Expected: nil channel: receive timed out (as expected)
```

## Step 2 -- Nil Channel in Select Is Skipped

When a channel variable is nil, its `select` case is permanently skipped — as if it doesn't exist.

```go
var active chan int    // nil — this case will be skipped
backup := make(chan int, 1)
backup <- 99

select {
case val := <-active:
    fmt.Println("active:", val) // never chosen — active is nil
case val := <-backup:
    fmt.Println("backup:", val) // always chosen
}
```

### Intermediate Verification
```bash
go run main.go
# Expected: backup: 99
```

## Step 3 -- Merge Two Channels with Nil Disabling

The core pattern: merge values from two channels until both are closed. When one closes, set it to nil so `select` stops trying to read from it.

```go
func merge(a, b <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
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
```

When `a` is closed, we set `a = nil`. The next iteration of the loop still enters `select`, but the `case <-a` is skipped because `a` is nil. Only `case <-b` is considered. When both are nil, the loop exits.

### Intermediate Verification
```bash
go run main.go
# Should print all values from both channels, then exit cleanly
```

## Step 4 -- Dynamic Enable/Disable in a State Machine

Use nil channels to model a system with changing capabilities. A worker alternates between "accepting jobs" and "paused" states.

```go
func statefulWorker(jobs <-chan string, pause, resume <-chan struct{}) {
    active := jobs // start accepting jobs

    for {
        select {
        case job, ok := <-active:
            if !ok {
                fmt.Println("Jobs channel closed, exiting")
                return
            }
            fmt.Println("Processing:", job)
        case <-pause:
            fmt.Println("Paused — no longer accepting jobs")
            active = nil // disable job processing
        case <-resume:
            fmt.Println("Resumed — accepting jobs again")
            active = jobs // re-enable job processing
        }
    }
}
```

### Intermediate Verification
```bash
go run main.go
# Worker processes jobs, pauses (stops processing), resumes, processes more
```

## Common Mistakes

### Forgetting That var Declares a Nil Channel
**Wrong:**
```go
var results chan int
go func() {
    results <- 42 // blocks forever — results is nil!
}()
```
**What happens:** The goroutine blocks permanently. If main also blocks waiting, you get a deadlock.
**Fix:** Always use `make(chan int)` to create a usable channel.

### Not Checking Both Channels Are Nil Before Exiting
**Wrong:**
```go
for {
    select {
    case val, ok := <-a:
        if !ok { return } // exits when a closes, ignoring remaining b values!
        process(val)
    case val, ok := <-b:
        if !ok { return }
        process(val)
    }
}
```
**What happens:** When `a` closes, you return immediately, losing all remaining values in `b`.
**Fix:** Set to nil instead of returning. Only exit when both are nil.

## Verify What You Learned

Build a priority merger in `main.go`:
1. Two channels: `highPriority` and `lowPriority`
2. A merger goroutine reads from both using `select`
3. When `highPriority` closes, set it to nil and continue draining `lowPriority`
4. When both are nil, close the output channel
5. Feed 3 high-priority messages and 5 low-priority messages
6. Print all merged messages with their priority label

Bonus: Observe that when both channels have data, `select` picks randomly. But when high-priority closes, only low-priority messages flow.

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
