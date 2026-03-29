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

## Step 1 -- Basic First-Result Race

Create multiple goroutines that simulate work with different durations and take the fastest result.

Edit `main.go` and implement the `raceSimple` function:

```go
func raceSimple() {
    fmt.Println("=== Simple Race ===")
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
    fmt.Printf("  Winner: %s\n\n", winner.value)
}
```

The channel is buffered so that losing goroutines can send their results without blocking, even after the consumer has moved on. Without the buffer, losers would leak.

### Intermediate Verification
```bash
go run main.go
```
Expected: one winner, which worker wins varies between runs:
```
=== Simple Race ===
  Winner: result from worker 2 (took 73ms)
```

## Step 2 -- Race with Cancellation

Use `context.WithCancel` to properly cancel losing goroutines:

```go
func raceWithCancel() {
    fmt.Println("=== Race with Cancellation ===")
    type result struct {
        value  int
        worker int
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    ch := make(chan result, 1)

    for i := 1; i <= 5; i++ {
        go func(id int) {
            // Simulate work in a cancelable way
            duration := time.Duration(rand.Intn(300)+100) * time.Millisecond
            select {
            case <-time.After(duration):
                // Work completed
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
    fmt.Printf("  Winner: worker %d with value %d\n", winner.worker, winner.value)

    time.Sleep(50 * time.Millisecond) // let cancel messages print
    fmt.Println()
}
```

After receiving the first result, `cancel()` triggers `ctx.Done()` in all goroutines, causing them to exit cleanly.

### Intermediate Verification
```bash
go run main.go
```
Expected: one winner, other workers report cancellation:
```
=== Race with Cancellation ===
  worker 3: canceled during work
  worker 5: canceled during work
  Winner: worker 1 with value 100
  worker 2: canceled before sending
  worker 4: canceled during work
```

## Step 3 -- The Or-Channel Function

Implement a reusable `or` function that takes multiple `<-chan struct{}` channels and returns a channel that closes when any of them closes. This is the general-purpose "first signal wins" combiner.

```go
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
```

This recursive implementation handles any number of channels. The `orDone` channel is passed into the recursive call so that when one branch triggers, the entire tree collapses.

### Intermediate Verification
```bash
go run main.go
```
Test with signals at different delays:
```
=== Or-Channel Function ===
  Signal received after ~100ms (fastest signal was 100ms)
```

## Step 4 -- Practical Application: Redundant Requests

Simulate a real-world scenario where you send the same request to multiple backend servers and use the fastest response:

```go
func redundantRequests() {
    fmt.Println("=== Redundant Requests ===")

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
    fmt.Printf("  Fastest: %s\n\n", resp.data)
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
ch := make(chan result) // unbuffered
for i := 0; i < 3; i++ {
    go func() { ch <- work() }()
}
winner := <-ch // only reads one value
// two goroutines are stuck trying to send
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

Implement a `fetchWithTimeout` function that races a simulated API call against a timeout. If the API responds within the timeout, return the result. If not, return a timeout error. Use `context.WithTimeout` and verify both the success and timeout paths.

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
