# 7. Heartbeat with Select

<!--
difficulty: advanced
concepts: [select, time.Ticker, heartbeat, health-monitoring, stall-detection]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [select-basics, select-in-for-loop, done-channel-pattern, time.Ticker]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01, 05, and 06 (select basics, for-select, done channel)
- Understanding of `time.Ticker` (periodic channel sends)

## Learning Objectives
- **Create** a heartbeat signal using `time.Ticker` inside a select loop
- **Build** a health monitor that detects stalled goroutines
- **Apply** the heartbeat pattern to a worker with variable processing time

## Why Heartbeats

In distributed systems, silence is ambiguous. If a component stops sending data, is it dead, blocked, or just idle? Without a heartbeat, you cannot tell. A heartbeat is a periodic "I am alive" signal that lets a supervisor distinguish between "no work to do" and "something is wrong."

In Go concurrency, the same problem exists at the goroutine level. A worker goroutine might deadlock on a channel, get stuck in an infinite loop, or block on a slow external call. The supervisor goroutine needs a way to detect this. The heartbeat pattern solves it: the worker sends a periodic signal on a dedicated channel. If the supervisor does not receive a heartbeat within the expected interval, it knows the worker is stalled.

This pattern is the foundation for circuit breakers, watchdog timers, and health check endpoints. It uses `time.Ticker` for periodic timing and `select` for multiplexing the heartbeat alongside work channels.

## Step 1 -- Basic Heartbeat with time.Ticker

Create a worker that sends a heartbeat on a dedicated channel at regular intervals, alongside doing work.

```go
done := make(chan struct{})
heartbeat := make(chan struct{}, 1) // Buffered: don't block worker if supervisor is slow
workResults := make(chan int)

go func() {
    defer close(workResults)
    ticker := time.NewTicker(200 * time.Millisecond)
    defer ticker.Stop()

    i := 0
    for {
        select {
        case <-done:
            return
        case <-ticker.C:
            select {
            case heartbeat <- struct{}{}:
            default: // Drop heartbeat if supervisor hasn't consumed the last one
            }
        case workResults <- i:
            i++
            time.Sleep(100 * time.Millisecond) // Simulate work
        }
    }
}()

// Consume results and heartbeats for a while
timeout := time.After(1 * time.Second)
for {
    select {
    case val, ok := <-workResults:
        if !ok {
            return
        }
        fmt.Println("result:", val)
    case <-heartbeat:
        fmt.Println("heartbeat received")
    case <-timeout:
        close(done)
        fmt.Println("stopping")
        return
    }
}
```

The heartbeat channel is buffered with capacity 1. If the supervisor has not consumed the last heartbeat, the worker drops the new one instead of blocking. This prevents the heartbeat mechanism from interfering with actual work.

### Intermediate Verification
Run the program. You should see interleaved "result" and "heartbeat received" messages for about 1 second, then "stopping".

## Step 2 -- Detecting a Stalled Worker

Build a supervisor that triggers an alert when the heartbeat stops arriving. Simulate a stall by having the worker block.

```go
done := make(chan struct{})
heartbeat := make(chan struct{}, 1)

go func() {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for i := 0; ; i++ {
        // Simulate a stall after 5 iterations
        if i == 5 {
            fmt.Println("worker: entering stall (blocked operation)")
            time.Sleep(5 * time.Second) // Simulate deadlock/slow call
        }

        select {
        case <-done:
            fmt.Println("worker: shutting down")
            return
        case <-ticker.C:
            select {
            case heartbeat <- struct{}{}:
            default:
            }
        default:
            time.Sleep(50 * time.Millisecond) // Simulate normal work
        }
    }
}()

// Supervisor: watch for missing heartbeats
const heartbeatTimeout = 500 * time.Millisecond
timer := time.NewTimer(heartbeatTimeout)
defer timer.Stop()

for {
    select {
    case <-heartbeat:
        fmt.Println("supervisor: heartbeat OK")
        if !timer.Stop() {
            select {
            case <-timer.C:
            default:
            }
        }
        timer.Reset(heartbeatTimeout)
    case <-timer.C:
        fmt.Println("supervisor: ALERT - worker stalled!")
        close(done)
        return
    }
}
```

The supervisor resets its timer every time it receives a heartbeat. If no heartbeat arrives within 500ms, the timer fires and the supervisor declares the worker stalled.

### Intermediate Verification
Run the program. You should see several "heartbeat OK" messages, then "worker: entering stall", then after 500ms "ALERT - worker stalled!".

## Step 3 -- Heartbeat-Emitting Worker Function

Encapsulate the pattern into a reusable worker function that returns a heartbeat channel.

```go
func heartbeatWorker(
    done <-chan struct{},
    pulseInterval time.Duration,
    work func(i int) int,
) (<-chan struct{}, <-chan int) {
    heartbeat := make(chan struct{}, 1)
    results := make(chan int)

    go func() {
        defer close(results)
        ticker := time.NewTicker(pulseInterval)
        defer ticker.Stop()

        i := 0
        for {
            select {
            case <-done:
                return
            case <-ticker.C:
                select {
                case heartbeat <- struct{}{}:
                default:
                }
            case results <- work(i):
                i++
            }
        }
    }()

    return heartbeat, results
}
```

Usage:

```go
done := make(chan struct{})

hb, results := heartbeatWorker(done, 200*time.Millisecond, func(i int) int {
    time.Sleep(80 * time.Millisecond) // Simulate work
    return i * i
})

timeout := time.After(1 * time.Second)
for {
    select {
    case <-hb:
        fmt.Println("pulse")
    case val := <-results:
        fmt.Println("result:", val)
    case <-timeout:
        close(done)
        fmt.Println("done")
        return
    }
}
```

The function returns read-only channels, encapsulating the heartbeat machinery. The caller only needs to listen for pulses and results.

### Intermediate Verification
Run the program. You should see squares (0, 1, 4, 9, 16...) and periodic "pulse" messages for 1 second.

## Common Mistakes

1. **Unbuffered heartbeat channel.** If the heartbeat channel is unbuffered, the worker blocks on the heartbeat send until the supervisor reads it. This couples the heartbeat mechanism to the supervisor's speed, which defeats the purpose.

2. **Heartbeat interval too close to detection timeout.** If the heartbeat fires every 100ms and the detection timeout is 150ms, normal scheduling jitter can cause false positives. The detection timeout should be at least 2-3x the heartbeat interval.

3. **Heartbeat blocking work.** The heartbeat send must be non-blocking (select with default). If the supervisor is slow and the heartbeat channel is full, the worker should drop the heartbeat, not stall.

4. **Not stopping the Ticker.** `time.NewTicker` creates a goroutine internally. If you do not call `Stop()`, it leaks. Always `defer ticker.Stop()`.

## Verify What You Learned

- [ ] Can you explain why the heartbeat channel should be buffered?
- [ ] Can you describe the relationship between heartbeat interval and detection timeout?
- [ ] Can you implement a supervisor that restarts a stalled worker?
- [ ] Can you explain how this pattern relates to TCP keepalive or HTTP health checks?

## What's Next
In the next exercise, you will build a general-purpose channel multiplexer that merges N channels into one, combining fan-in with the select patterns you have learned.

## Summary
The heartbeat pattern uses a `time.Ticker` inside a for-select loop to send periodic "alive" signals on a dedicated channel. The supervisor monitors this channel and triggers alerts when heartbeats stop arriving. The heartbeat channel must be buffered to prevent blocking the worker. The detection timeout should be significantly larger than the heartbeat interval to avoid false positives. This pattern is the goroutine-level equivalent of health checks in distributed systems.

## Reference
- [time.NewTicker](https://pkg.go.dev/time#NewTicker)
- [Concurrency in Go (Katherine Cox-Buday) - Heartbeat pattern](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Go Concurrency Patterns: Timing out](https://go.dev/blog/concurrency-timeouts)
