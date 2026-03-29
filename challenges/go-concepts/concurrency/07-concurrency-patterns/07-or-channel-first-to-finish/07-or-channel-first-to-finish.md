# 7. Or-Channel: First to Finish

<!--
difficulty: advanced
concepts: [or-channel, speculative execution, cancellation, select, redundant work]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channels, select, context, done channel pattern]
-->

## Prerequisites
- Go 1.22+ installed
- Strong understanding of goroutines, channels, and `select`
- Familiarity with `context.Context` for cancellation
- Understanding of the done-channel pattern (exercise 06)

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** the or-channel pattern to race multiple goroutines
- **Cancel** losing goroutines after the first result arrives
- **Apply** speculative execution for latency optimization
- **Analyze** the trade-offs of redundant work vs lower tail latency

## Why Or-Channel (First to Finish)
The or-channel pattern runs the same (or equivalent) work in multiple goroutines and takes whichever result comes first, canceling the rest. This is speculative execution: you trade extra CPU for lower latency by hedging your bets.

Real-world use cases include: sending the same request to multiple replicas and using the fastest response; trying multiple algorithms for the same problem; racing a cache lookup against a database query. Google famously uses this pattern to reduce tail latency -- if one server is slow, the redundant request to another server saves the user from waiting.

The pattern has three parts: launch N goroutines doing equivalent work, select the first result from any of them, and cancel the rest immediately. Without proper cancellation, the losing goroutines waste resources running to completion.

```
  Or-Channel Data Flow

  request ---> server 1 (slow)     --+
           --> server 2 (fast) ------+--> take first, cancel rest
           --> server 3 (medium)  --+

  The fastest response wins. Others are canceled via context.
```

## Step 1 -- Basic First-Result Race

Create multiple goroutines that simulate work with different durations and take the fastest result.

```go
package main

import (
    "fmt"
    "math/rand"
    "time"
)

func main() {
    type result struct {
        value  string
        source int
    }

    ch := make(chan result, 3) // buffered to avoid goroutine leak on losers

    for i := 1; i <= 3; i++ {
        go func(id int) {
            duration := time.Duration(rand.Intn(200)+50) * time.Millisecond
            time.Sleep(duration)
            ch <- result{
                value:  fmt.Sprintf("result from worker %d (took %v)", id, duration),
                source: id,
            }
        }(i)
    }

    winner := <-ch
    fmt.Printf("Winner: %s\n", winner.value)
}
```

The channel is buffered so that losing goroutines can send their results without blocking, even after the consumer has moved on. Without the buffer, losers would leak.

### Intermediate Verification
```bash
go run main.go
```
Expected: one winner, which worker wins varies between runs:
```
Winner: result from worker 2 (took 73ms)
```

## Step 2 -- Race with Cancellation

Use `context.WithCancel` to properly cancel losing goroutines:

```go
package main

import (
    "context"
    "fmt"
    "math/rand"
    "time"
)

func main() {
    type result struct {
        value  int
        worker int
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    ch := make(chan result, 1)

    for i := 1; i <= 5; i++ {
        go func(id int) {
            duration := time.Duration(rand.Intn(300)+100) * time.Millisecond
            select {
            case <-time.After(duration):
                select {
                case ch <- result{value: id * 100, worker: id}:
                case <-ctx.Done():
                    fmt.Printf("  worker %d: canceled before sending\n", id)
                    return
                }
            case <-ctx.Done():
                fmt.Printf("  worker %d: canceled during work\n", id)
                return
            }
        }(i)
    }

    winner := <-ch
    cancel() // cancel all remaining workers
    fmt.Printf("Winner: worker %d with value %d\n", winner.worker, winner.value)

    time.Sleep(50 * time.Millisecond) // let cancel messages print
}
```

After receiving the first result, `cancel()` triggers `ctx.Done()` in all goroutines, causing them to exit cleanly.

### Intermediate Verification
```bash
go run main.go
```
Expected: one winner, other workers report cancellation:
```
  worker 3: canceled during work
  Winner: worker 1 with value 100
  worker 4: canceled during work
```

## Step 3 -- The Or-Channel Function

Implement a reusable `or` function that takes multiple `<-chan struct{}` channels and returns a channel that closes when any of them closes. This is the general-purpose "first signal wins" combiner.

```go
package main

import (
    "fmt"
    "time"
)

func or(channels ...<-chan struct{}) <-chan struct{} {
    switch len(channels) {
    case 0:
        return nil
    case 1:
        return channels[0]
    }

    orDone := make(chan struct{})
    go func() {
        defer close(orDone)
        switch len(channels) {
        case 2:
            select {
            case <-channels[0]:
            case <-channels[1]:
            }
        default:
            select {
            case <-channels[0]:
            case <-channels[1]:
            case <-channels[2]:
            case <-or(append(channels[3:], orDone)...):
            }
        }
    }()
    return orDone
}

func sig(after time.Duration) <-chan struct{} {
    ch := make(chan struct{})
    go func() {
        defer close(ch)
        time.Sleep(after)
    }()
    return ch
}

func main() {
    start := time.Now()
    <-or(
        sig(2*time.Second),
        sig(500*time.Millisecond),
        sig(1*time.Second),
        sig(100*time.Millisecond), // fastest
        sig(3*time.Second),
    )
    fmt.Printf("Signal received after %v (fastest was 100ms)\n",
        time.Since(start).Round(time.Millisecond))
}
```

This recursive implementation handles any number of channels. The `orDone` channel is passed into the recursive call so that when one branch triggers, the entire tree collapses.

### Intermediate Verification
```bash
go run main.go
```
```
Signal received after 100ms (fastest was 100ms)
```

## Step 4 -- Practical Application: Redundant Requests

Simulate sending the same request to multiple backend servers and using the fastest response:

```go
package main

import (
    "context"
    "fmt"
    "math/rand"
    "time"
)

func main() {
    queryServer := func(ctx context.Context, serverID int) (string, error) {
        latency := time.Duration(rand.Intn(400)+100) * time.Millisecond
        select {
        case <-time.After(latency):
            return fmt.Sprintf("data from server %d (%v)", serverID, latency), nil
        case <-ctx.Done():
            return "", ctx.Err()
        }
    }

    type response struct {
        data string
        err  error
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    ch := make(chan response, 3)
    for _, id := range []int{1, 2, 3} {
        go func(serverID int) {
            data, err := queryServer(ctx, serverID)
            select {
            case ch <- response{data, err}:
            case <-ctx.Done():
            }
        }(id)
    }

    resp := <-ch
    cancel()
    fmt.Printf("Fastest: %s\n", resp.data)
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: the fastest server's response, varying between runs.

## Common Mistakes

### Unbuffered Channel Causes Goroutine Leaks
**Wrong:**
```go
package main

import "fmt"

func main() {
    type result struct{ v int }
    ch := make(chan result) // unbuffered
    for i := 0; i < 3; i++ {
        go func(id int) { ch <- result{id} }(i)
    }
    winner := <-ch
    fmt.Println(winner)
    // two goroutines are stuck trying to send forever
}
```
**What happens:** The losing goroutines block on send forever because nobody reads their values.

**Fix:** Either buffer the channel to hold all results, or use context cancellation to stop losers.

### Not Canceling Losing Goroutines
**Wrong:**
```go
winner := <-ch
// forget to cancel -- losing goroutines run to completion
```
**What happens:** Losing goroutines waste CPU and memory completing work whose result is discarded.

**Fix:** Use `context.WithCancel` and call `cancel()` after receiving the first result.

### Race Condition on the Result Channel
If multiple goroutines finish at the same instant, only one value is read. The others either block (unbuffered) or sit in the buffer (buffered). This is correct behavior -- you wanted only the first -- but make sure your channel and cancellation strategy handle it.

## Verify What You Learned

Run `go run main.go` and verify:
- Simple race: one winner reported
- Race with cancellation: winner plus cancellation messages from losers
- Or-channel: signal received in ~100ms (the fastest)
- Redundant requests: fastest server responds
- Fetch with timeout: some succeed, some time out (200ms limit)

## What's Next
Continue to [08-tee-channel-split-stream](../08-tee-channel-split-stream/08-tee-channel-split-stream.md) to learn how to duplicate a channel stream for parallel processing.

## Summary
- The or-channel pattern races N goroutines and takes the first result
- Buffer result channels or use cancellation to prevent goroutine leaks from losers
- `context.WithCancel` provides clean cancellation of losing goroutines
- The recursive `or` function combines N signal channels into one
- Speculative execution trades CPU for lower tail latency
- Always cancel remaining work after receiving the winning result

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
- [Advanced Go Concurrency Patterns](https://www.youtube.com/watch?v=QDDwwePbDtw)
- [The tail at scale (Google)](https://research.google/pubs/pub40801/) -- the paper motivating redundant requests
