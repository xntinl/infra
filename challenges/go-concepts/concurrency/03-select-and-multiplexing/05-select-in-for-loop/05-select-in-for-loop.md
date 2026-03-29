# 5. Select in For Loop

<!--
difficulty: intermediate
concepts: [select, for-select, event-loop, quit-channel, goroutine-lifecycle]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [select-basics, select-with-default, channels, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-02 (select basics, select with default)
- Understanding of channel close semantics

## Learning Objectives
- **Build** a continuous event loop with `for` + `select`
- **Handle** multiple event sources in a single goroutine
- **Implement** a quit channel for clean shutdown

## Why For-Select

A single `select` handles one event and returns. Most goroutines need to handle events continuously: a server processes requests until shutdown, a worker reads tasks until the queue closes, a monitor checks health until told to stop.

The `for` + `select` combination is the standard Go event loop. It is the idiomatic way to write a goroutine that reacts to multiple channels over its entire lifetime. Nearly every long-running goroutine in production Go code follows this pattern.

The quit channel is the clean shutdown mechanism. Instead of killing a goroutine externally (which Go intentionally does not support), you send a signal on a channel that the goroutine checks in its `select`. This gives the goroutine a chance to clean up resources before exiting. This pattern is so common that it was formalized into `context.Context`, which you will learn in a later section.

## Step 1 -- Basic Event Loop

Build a goroutine that listens on a work channel and a quit channel in a loop.

```go
work := make(chan string)
quit := make(chan struct{})

go func() {
    for {
        select {
        case task := <-work:
            fmt.Println("processing:", task)
        case <-quit:
            fmt.Println("shutting down")
            return
        }
    }
}()

work <- "task-1"
work <- "task-2"
work <- "task-3"
close(quit)

time.Sleep(100 * time.Millisecond) // Let goroutine finish
```

The goroutine loops forever, processing tasks as they arrive. When `quit` is closed, the `<-quit` case succeeds (closed channels return the zero value immediately), and the goroutine returns.

### Intermediate Verification
Run the program. You should see all three tasks processed followed by "shutting down".

## Step 2 -- Multiple Event Sources

Extend the event loop to handle different types of events from different channels.

```go
orders := make(chan string, 5)
alerts := make(chan string, 5)
quit := make(chan struct{})

// Simulate event producers
go func() {
    for i := 0; i < 5; i++ {
        orders <- fmt.Sprintf("order-%d", i)
        time.Sleep(30 * time.Millisecond)
    }
}()

go func() {
    for i := 0; i < 3; i++ {
        alerts <- fmt.Sprintf("alert-%d", i)
        time.Sleep(50 * time.Millisecond)
    }
}()

// Event loop
go func() {
    time.Sleep(300 * time.Millisecond)
    close(quit)
}()

for {
    select {
    case order := <-orders:
        fmt.Println("[ORDER]", order)
    case alert := <-alerts:
        fmt.Println("[ALERT]", alert)
    case <-quit:
        fmt.Println("event loop stopped")
        return
    }
}
```

A single `select` cleanly multiplexes two event streams plus a shutdown signal. Adding a new event source is as simple as adding a new case.

### Intermediate Verification
Run the program. You should see interleaved order and alert messages, ending with "event loop stopped".

## Step 3 -- Clean Exit with Channel Close Detection

Use the two-value receive form `val, ok := <-ch` to detect when a producer closes its channel, and exit only when all producers are done.

```go
source1 := make(chan int)
source2 := make(chan int)

go func() {
    for i := 0; i < 3; i++ {
        source1 <- i
        time.Sleep(50 * time.Millisecond)
    }
    close(source1)
}()

go func() {
    for i := 10; i < 14; i++ {
        source2 <- i
        time.Sleep(30 * time.Millisecond)
    }
    close(source2)
}()

s1Done, s2Done := false, false

for {
    select {
    case val, ok := <-source1:
        if !ok {
            source1 = nil // Nil channel is never selected
            s1Done = true
        } else {
            fmt.Println("source1:", val)
        }
    case val, ok := <-source2:
        if !ok {
            source2 = nil
            s2Done = true
        } else {
            fmt.Println("source2:", val)
        }
    }

    if s1Done && s2Done {
        fmt.Println("all sources closed")
        break
    }
}
```

Key technique: setting a channel to `nil` after it closes. A `nil` channel in a `select` case is never ready, so the runtime skips it. This prevents the closed channel from being selected repeatedly (which would return zero values in a tight loop).

### Intermediate Verification
Run the program. You should see values from both sources, then "all sources closed". No zero values or infinite loops.

## Common Mistakes

1. **Not setting closed channels to nil.** A closed channel returns the zero value immediately, forever. Without setting it to `nil`, the `select` will spin on the closed channel case.

2. **Breaking out of select vs. the for loop.** A `break` inside a `select` breaks out of the `select`, not the enclosing `for` loop. Use `return`, a labeled break, or a flag variable to exit the loop.

3. **Goroutine leak: forgetting the quit channel.** If the for-select loop has no exit condition, the goroutine runs forever. Every for-select must have a way to terminate: a quit channel, context cancellation, or detection of all sources closing.

4. **Sending on a closed channel.** Closing a channel signals all receivers, but sending on a closed channel panics. The producer closes, the consumer detects.

## Verify What You Learned

- [ ] Can you explain why `break` inside a `select` does not exit the `for` loop?
- [ ] Can you explain the nil channel trick and why it is necessary?
- [ ] Can you list three ways a for-select loop can terminate?

## What's Next
In the next exercise, you will learn the done channel pattern -- a formalization of the quit channel concept that enables cancellation propagation across goroutine trees.

## Summary
The `for` + `select` combination is Go's event loop idiom. A goroutine loops forever, using `select` to multiplex across work channels, event streams, and a quit/done channel. When a channel closes, set it to `nil` to prevent the select from spinning on zero values. Every for-select loop must have an exit path to prevent goroutine leaks.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Go Spec: Receive operator](https://go.dev/ref/spec#Receive_operator)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
