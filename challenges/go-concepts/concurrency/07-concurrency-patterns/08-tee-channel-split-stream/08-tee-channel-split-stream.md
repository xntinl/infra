# 8. Tee-Channel: Split Stream

<!--
difficulty: advanced
concepts: [tee channel, stream splitting, backpressure, data duplication]
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
- **Analyze** how backpressure from a slow consumer affects the entire tee
- **Handle** cancellation in a tee to prevent goroutine leaks
- **Apply** stream splitting for parallel processing of the same data

## Why Tee-Channel
The tee-channel pattern takes one input stream and duplicates it into two output streams. Every value from the input appears in both outputs. This is analogous to the Unix `tee` command, which reads from stdin and writes to both stdout and a file simultaneously.

Use cases include: logging a data stream while also processing it; feeding the same data to two different analysis pipelines; duplicating events for both real-time processing and archival; splitting a stream for comparison (sending the same input through two different algorithms).

The challenge is backpressure. Since both output channels must receive every value, the tee runs at the speed of the slowest consumer. If one consumer is slow, the fast consumer also slows down because the tee cannot send the next value until both consumers have received the current one. Understanding and managing this behavior is key to using the pattern effectively.

## Step 1 -- Basic Tee Function

Implement a tee that reads from one input and sends each value to two outputs.

Edit `main.go` and implement the `tee` function:

```go
func tee(done <-chan struct{}, in <-chan int) (<-chan int, <-chan int) {
    out1 := make(chan int)
    out2 := make(chan int)

    go func() {
        defer close(out1)
        defer close(out2)
        for val := range in {
            // Local copies for the select cases below.
            // We need to nil out channels after sending to ensure
            // both receive the value before moving to the next.
            o1, o2 := out1, out2
            for count := 0; count < 2; count++ {
                select {
                case o1 <- val:
                    o1 = nil // already sent to out1
                case o2 <- val:
                    o2 = nil // already sent to out2
                case <-done:
                    return
                }
            }
        }
    }()

    return out1, out2
}
```

The inner loop with `count < 2` ensures both outputs receive the value. After sending to one, that channel variable is set to nil (a nil channel blocks forever in select), forcing the next iteration to send to the other.

### Intermediate Verification
```bash
go run main.go
```
Expected: both consumers receive the same values:
```
=== Basic Tee ===
  Consumer 1 received: 1
  Consumer 2 received: 1
  Consumer 1 received: 2
  Consumer 2 received: 2
  ...
```

## Step 2 -- Tee with Backpressure Demonstration

Show how a slow consumer affects the entire tee:

```go
func backpressureDemo() {
    fmt.Println("=== Backpressure Demo ===")
    done := make(chan struct{})
    defer close(done)

    // Generator
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

    // Fast consumer
    go func() {
        defer wg.Done()
        for v := range out1 {
            fmt.Printf("  fast consumer: got %d at %v\n", v, time.Now().Format("04:05.000"))
        }
    }()

    // Slow consumer
    go func() {
        defer wg.Done()
        for v := range out2 {
            fmt.Printf("  slow consumer: got %d at %v\n", v, time.Now().Format("04:05.000"))
            time.Sleep(200 * time.Millisecond) // slow!
        }
    }()

    wg.Wait()
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Observe that the fast consumer receives values at the same pace as the slow one. The timestamps reveal the bottleneck.

## Step 3 -- Buffered Tee to Decouple Consumers

Mitigate backpressure by adding buffer between the tee and a slow consumer:

```go
func bufferedConsumer(done <-chan struct{}, in <-chan int, bufSize int) <-chan int {
    out := make(chan int, bufSize)
    go func() {
        defer close(out)
        for {
            select {
            case v, ok := <-in:
                if !ok {
                    return
                }
                select {
                case out <- v:
                case <-done:
                    return
                }
            case <-done:
                return
            }
        }
    }()
    return out
}
```

Place this between the tee output and the slow consumer. The buffer absorbs the speed difference up to its capacity.

### Intermediate Verification
```bash
go run main.go
```
With a buffer of 5, the fast consumer should no longer be blocked by the slow one (for the first 5 values).

## Step 4 -- Practical Application: Log and Process

Build a pipeline that tees a stream for both logging and processing:

```go
func logAndProcess() {
    fmt.Println("=== Log and Process ===")
    done := make(chan struct{})
    defer close(done)

    // Source: generate events
    events := make(chan int)
    go func() {
        defer close(events)
        for i := 1; i <= 8; i++ {
            events <- i
        }
    }()

    // Tee: split into log stream and process stream
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

    // Processor: only processes even events, does "heavy" work
    go func() {
        defer wg.Done()
        for event := range processStream {
            if event%2 == 0 {
                fmt.Printf("  [PROCESS] event=%d -> result=%d\n", event, event*event)
            }
        }
    }()

    wg.Wait()
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: all events logged, only even events processed.

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

Implement a `tee3` function that splits one input into three outputs. Use the same nil-channel select technique but with `count < 3` and three output channels. Test it with a generator and three consumers with different speeds.

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
