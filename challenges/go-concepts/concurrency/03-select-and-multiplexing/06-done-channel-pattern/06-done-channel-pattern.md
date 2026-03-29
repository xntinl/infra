# 6. Done Channel Pattern

<!--
difficulty: intermediate
concepts: [done-channel, cancellation, close-broadcast, goroutine-lifecycle, context-foundation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [select-basics, select-in-for-loop, channels, goroutines, channel-close]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 and 05 (select basics, for-select loop)
- Understanding of channel close semantics (a closed channel returns zero value immediately)

## Learning Objectives
- **Implement** a done channel to signal cancellation to one or more goroutines
- **Explain** why closing a channel is a broadcast mechanism
- **Propagate** cancellation across a tree of goroutines

## Why Done Channels

Goroutines are not preemptible in the traditional OS sense. You cannot kill a goroutine from outside. The only way to stop a goroutine is to make it stop itself by giving it a signal it checks voluntarily. The done channel is that signal.

When you close a channel, every receiver waiting on it unblocks immediately. This makes close a broadcast operation: one close wakes up an unlimited number of listeners. A done channel exploits this property. You create a `chan struct{}` (zero-size, carries no data), pass it to all goroutines, and close it when you want them to stop. Every goroutine that checks this channel in its `select` will see the close and can exit cleanly.

This pattern is so fundamental that it was formalized into `context.Context` in Go 1.7. The `ctx.Done()` method returns exactly this kind of channel. Understanding the raw done channel pattern gives you deep intuition for how context cancellation works under the hood.

## Step 1 -- Single Goroutine Cancellation

Create a worker goroutine that runs until a done channel is closed.

```go
done := make(chan struct{})
results := make(chan int)

go func() {
    defer close(results)
    i := 0
    for {
        select {
        case <-done:
            fmt.Println("worker: received cancellation")
            return
        case results <- i:
            i++
            time.Sleep(50 * time.Millisecond)
        }
    }
}()

// Consume a few results
for i := 0; i < 5; i++ {
    fmt.Println("received:", <-results)
}

// Signal the worker to stop
close(done)
time.Sleep(100 * time.Millisecond)
fmt.Println("main: worker stopped")
```

The worker produces values until the done channel is closed. The main goroutine consumes 5 values, then signals cancellation. The worker detects it via the `<-done` case and returns.

### Intermediate Verification
Run the program. You should see 5 values, then "worker: received cancellation" and "main: worker stopped".

## Step 2 -- Broadcasting Cancellation to Multiple Goroutines

Close one channel to stop multiple goroutines simultaneously.

```go
done := make(chan struct{})
var wg sync.WaitGroup

worker := func(id int) {
    defer wg.Done()
    for {
        select {
        case <-done:
            fmt.Printf("worker %d: stopping\n", id)
            return
        default:
            fmt.Printf("worker %d: working\n", id)
            time.Sleep(100 * time.Millisecond)
        }
    }
}

for i := 1; i <= 3; i++ {
    wg.Add(1)
    go worker(i)
}

time.Sleep(350 * time.Millisecond)
fmt.Println("main: cancelling all workers")
close(done)
wg.Wait()
fmt.Println("main: all workers stopped")
```

One `close(done)` stops all three workers. This is the power of the broadcast property: you do not need to track or signal each goroutine individually.

### Intermediate Verification
Run the program. You should see interleaved "working" messages from all 3 workers, followed by all 3 "stopping" messages after cancellation.

## Step 3 -- Propagating Cancellation Through a Pipeline

Build a two-stage pipeline where cancellation flows from the top through all stages.

```go
done := make(chan struct{})
var wg sync.WaitGroup

// Stage 1: generates numbers
stage1Out := make(chan int)
wg.Add(1)
go func() {
    defer wg.Done()
    defer close(stage1Out)
    i := 0
    for {
        select {
        case <-done:
            fmt.Println("stage1: cancelled")
            return
        case stage1Out <- i:
            i++
            time.Sleep(50 * time.Millisecond)
        }
    }
}()

// Stage 2: doubles numbers
stage2Out := make(chan int)
wg.Add(1)
go func() {
    defer wg.Done()
    defer close(stage2Out)
    for {
        select {
        case <-done:
            fmt.Println("stage2: cancelled")
            return
        case val, ok := <-stage1Out:
            if !ok {
                return
            }
            select {
            case <-done:
                return
            case stage2Out <- val * 2:
            }
        }
    }
}()

// Consumer
for i := 0; i < 5; i++ {
    val := <-stage2Out
    fmt.Println("consumed:", val)
}

close(done)
wg.Wait()
fmt.Println("pipeline shut down cleanly")
```

Both stages check the same done channel. When the consumer closes it, both stages exit. The `sync.WaitGroup` ensures the main goroutine waits for all stages to finish cleanup before proceeding.

### Intermediate Verification
Run the program. You should see 5 consumed values (0, 2, 4, 6, 8), then both stages reporting cancellation, then "pipeline shut down cleanly".

## Common Mistakes

1. **Sending a value on the done channel instead of closing it.** Sending a value only wakes one receiver. If you have 5 goroutines, you would need to send 5 values. Closing is the correct approach because it wakes all receivers.

2. **Using `chan bool` instead of `chan struct{}`.** Both work, but `chan struct{}` communicates intent: this channel carries a signal, not data. It also has zero allocation cost per element.

3. **Checking done outside of select.** A direct `<-done` blocks until the channel is closed. It must be inside a `select` alongside the work channel so the goroutine can do work while also being responsive to cancellation.

4. **Forgetting to check done on both sides of a pipeline stage.** A stage that reads from an input channel and writes to an output channel needs done checks on both operations. Otherwise, it can block on a write after cancellation was signaled.

## Verify What You Learned

- [ ] Can you explain why close is a broadcast and send is not?
- [ ] Can you explain why `chan struct{}` is preferred over `chan bool`?
- [ ] Can you describe how to propagate cancellation through a multi-stage pipeline?
- [ ] Can you identify where `context.Context` replaces this pattern?

## What's Next
In the next exercise, you will build a heartbeat mechanism using `select` and `time.Ticker` to monitor whether goroutines are alive and responsive.

## Summary
The done channel pattern uses a closed `chan struct{}` as a broadcast cancellation signal. Closing the channel wakes all goroutines that check it in their `select` loops. This is the manual implementation of what `context.Context` provides. Every goroutine should check a done channel alongside its work channels to remain responsive to cancellation. Use `sync.WaitGroup` to wait for all goroutines to finish cleanup.

## Reference
- [Go Concurrency Patterns: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go Spec: Close](https://go.dev/ref/spec#Close)
- [context package](https://pkg.go.dev/context)
