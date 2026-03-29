# 4. Fix Race with Channel

<!--
difficulty: intermediate
concepts: [channels, ownership, share by communicating, goroutine confinement]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, data race concept, race detector]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-03 (data races, race detector, mutex fix)
- Understanding of Go channels (send, receive, buffered/unbuffered)

## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race by funneling writes through a channel to a single owner goroutine
- **Apply** the Go proverb: "Don't communicate by sharing memory; share memory by communicating"
- **Verify** the fix using the `-race` flag
- **Compare** the channel approach with the mutex approach

## Why Channels
The mutex approach from exercise 03 works by allowing multiple goroutines to access shared memory, but serializing their access with locks. The channel approach takes a fundamentally different perspective: instead of sharing a variable and protecting it, you give ownership of the variable to a single goroutine and have all other goroutines communicate with it through channels.

This is the Go philosophy captured in the proverb: **"Don't communicate by sharing memory; share memory by communicating."**

When a single goroutine owns the data, there is no concurrent access, so there is no race. The channel serves as both the communication mechanism and the synchronization mechanism. This pattern is more idiomatic in Go and often leads to clearer, more composable designs.

## Step 1 -- Single Owner with Channel

Edit `main.go` and implement `safeCounterChannel`. The idea: one goroutine owns the counter. Worker goroutines send increment requests through a channel. The owner reads from the channel and applies the increments:

```go
func safeCounterChannel() int {
    increments := make(chan struct{}, 100) // buffered to reduce blocking
    done := make(chan int)

    // Owner goroutine: sole writer of the counter
    go func() {
        counter := 0
        for range increments {
            counter++
        }
        done <- counter
    }()

    // Worker goroutines: send increment signals
    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                increments <- struct{}{}
            }
        }()
    }

    wg.Wait()
    close(increments) // signal to owner: no more increments
    return <-done      // wait for owner to return final count
}
```

Key observations:
- Only the owner goroutine reads and writes `counter` -- no concurrent access
- `close(increments)` causes the `range` loop to exit
- `done <- counter` sends the final value back to the caller

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Fix Race with Channel ===
Racy counter:  <wrong number>
Safe (channel): 1000000
```

## Step 2 -- Verify with the Race Detector

```bash
go run -race main.go
```

The race detector should report races only for `racyCounter`, NOT for `safeCounterChannel`. The channel send/receive operations establish happens-before relationships that the detector recognizes.

### Intermediate Verification
Confirm zero race warnings from `safeCounterChannel`.

## Step 3 -- Compare Mutex vs Channel

Implement `compareMutexAndChannel` to time both approaches:

```go
func compareMutexAndChannel() {
    fmt.Println("\n=== Mutex vs Channel Timing ===")

    start := time.Now()
    resultMutex := safeCounterMutex()
    mutexDuration := time.Since(start)

    start = time.Now()
    resultChannel := safeCounterChannel()
    channelDuration := time.Since(start)

    fmt.Printf("Mutex:   %d in %v\n", resultMutex, mutexDuration)
    fmt.Printf("Channel: %d in %v\n", resultChannel, channelDuration)

    if channelDuration > mutexDuration {
        fmt.Printf("Channel is %.1fx slower (expected for fine-grained operations)\n",
            float64(channelDuration)/float64(mutexDuration))
    } else {
        fmt.Printf("Mutex is %.1fx slower\n",
            float64(mutexDuration)/float64(channelDuration))
    }
}
```

For this specific problem (incrementing a counter), the channel approach will likely be slower because each increment requires a channel send/receive, which is heavier than a mutex lock/unlock. That is expected. The channel pattern shines when the owned data structure is more complex or when the communication pattern itself carries meaningful messages (not just "increment").

### Intermediate Verification
```bash
go run main.go
```
Both should produce 1,000,000. The channel version will typically be slower for this use case.

## Common Mistakes

### Forgetting to Close the Channel
**Wrong:**
```go
wg.Wait()
// forgot close(increments)
return <-done // deadlock: owner is still ranging over increments
```
**What happens:** The owner goroutine blocks forever on `range increments`, and `done` is never sent, causing a deadlock.

**Fix:** Always `close(increments)` after all senders are done (after `wg.Wait()`).

### Closing the Channel Before All Sends Complete
**Wrong:**
```go
go func() {
    defer wg.Done()
    for j := 0; j < 1000; j++ {
        increments <- struct{}{}
    }
    close(increments) // BUG: other goroutines are still sending!
}()
```
**What happens:** Sending on a closed channel causes a panic.

**Fix:** Close the channel once from the coordinating goroutine, after `wg.Wait()` confirms all senders have finished.

### Using Unbuffered Channel with Many Senders
An unbuffered channel blocks the sender until the receiver is ready. With many senders doing fine-grained operations, this creates excessive synchronization overhead. Use a buffered channel to allow senders to proceed without waiting for each receive.

## Verify What You Learned

1. Run `go run -race main.go` and confirm zero race warnings for the channel version
2. Why is there no race on `counter` in `safeCounterChannel`?
3. When would you prefer the channel approach over a mutex?
4. What Go proverb does this pattern embody?

## What's Next
Continue to [05-fix-race-with-atomic](../05-fix-race-with-atomic/05-fix-race-with-atomic.md) to fix the same race using `sync/atomic`.

## Summary
- The channel approach eliminates races by giving data ownership to a single goroutine
- Worker goroutines communicate through channels instead of accessing shared memory
- "Don't communicate by sharing memory; share memory by communicating"
- `close(channel)` must happen after all senders are done, and before receiving the final result
- For simple counters, channels have more overhead than mutexes, but the pattern scales to complex state
- The race detector recognizes channel operations as synchronization points

## Reference
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Proverbs](https://go-proverbs.github.io/)
