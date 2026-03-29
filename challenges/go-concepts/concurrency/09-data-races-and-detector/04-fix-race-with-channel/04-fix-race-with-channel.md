---
difficulty: intermediate
concepts: [channels, ownership, share by communicating, goroutine confinement, batching]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, data race concept, race detector]
---

# 4. Fix Race with Channel


## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race by funneling writes through a channel to a single owner goroutine
- **Apply** the Go proverb: "Don't communicate by sharing memory; share memory by communicating"
- **Optimize** channel-based solutions with batching
- **Compare** the channel approach with the mutex approach

## Why Channels

The mutex approach from exercise 03 works by allowing multiple goroutines to access shared memory, but serializing their access with locks. The channel approach takes a fundamentally different perspective: instead of sharing a variable and protecting it, you give **ownership** of the variable to a single goroutine and have all other goroutines communicate with it through channels.

This is the Go philosophy captured in the proverb: **"Don't communicate by sharing memory; share memory by communicating."**

When a single goroutine owns the data, there is no concurrent access, so there is no race. The channel serves as both the communication mechanism and the synchronization mechanism.

## Step 1 -- Single Owner with Channel

The owner goroutine holds the counter. Workers send increment signals through a channel:

```go
package main

import (
    "fmt"
    "sync"
)

func safeCounterChannel() int {
    // Buffered channel reduces blocking: workers can send without waiting
    // for the owner to process each signal immediately.
    increments := make(chan struct{}, 100)
    done := make(chan int)

    // Owner goroutine: the SOLE writer/reader of counter.
    go func() {
        counter := 0
        for range increments {
            counter++
        }
        done <- counter
    }()

    // Worker goroutines: send increment signals, never touch counter.
    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                increments <- struct{}{} // signal "please increment"
            }
        }()
    }

    // After all workers finish, close the channel to tell the owner
    // that no more increments are coming.
    wg.Wait()
    close(increments)

    // The owner sends the final count through done after range exits.
    return <-done
}

func main() {
    result := safeCounterChannel()
    fmt.Printf("Result: %d (expected 1000000)\n", result)
}
```

Key observations:
- Only the owner goroutine reads and writes `counter` -- no concurrent access
- `close(increments)` causes the `range` loop to exit
- `done <- counter` sends the final value back to the caller
- The buffered channel (capacity 100) allows workers to send without waiting for each receive

### Verification
```bash
go run -race main.go
```
Expected: 1,000,000 with zero race warnings from `safeCounterChannel`.

## Step 2 -- Batched Channel (Practical Optimization)

Sending one million channel signals is slow. In practice, each worker should compute locally and send a single batch result:

```go
package main

import (
    "fmt"
    "sync"
)

func safeCounterChannelBatched() int {
    partialCounts := make(chan int, 1000)

    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // Count locally (no shared state) then send once.
            localCount := 0
            for j := 0; j < 1000; j++ {
                localCount++
            }
            partialCounts <- localCount
        }()
    }

    go func() {
        wg.Wait()
        close(partialCounts)
    }()

    total := 0
    for count := range partialCounts {
        total += count
    }
    return total
}

func main() {
    result := safeCounterChannelBatched()
    fmt.Printf("Result: %d (expected 1000000)\n", result)
}
```

### Verification
```bash
go run -race main.go
```
Expected: 1,000,000 with zero race warnings. Much faster than the 1-by-1 approach.

## Step 3 -- Timing Comparison

The full `main.go` compares mutex, channel (1-by-1), and channel (batched):

### Verification
```bash
go run main.go
```
Sample output:
```
=== Timing Comparison ===
  Mutex:                 248.3ms
  Channel (1-by-1):      1.82s
  Channel (batched):     312.5ms
Channel (1-by-1) is ~7x slower than mutex for fine-grained ops.
Batched channel is comparable because it reduces channel traffic.
```

For this specific problem (incrementing a counter), the 1-by-1 channel approach is slower because each increment requires a channel send/receive, which is heavier than a mutex lock/unlock. The batched approach reduces this overhead dramatically.

The channel pattern shines when:
- The owned data structure is complex (not just a counter)
- The communication itself carries meaningful messages (not just "increment")
- You need ownership transfer between stages (pipelines)

## Common Mistakes

### Forgetting to Close the Channel
```go
wg.Wait()
// forgot close(increments)
return <-done // DEADLOCK: owner is still ranging over increments
```
The owner goroutine blocks forever on `range increments`, and `done` is never sent. Always `close(increments)` after all senders are done.

### Closing the Channel Before All Sends Complete
```go
go func() {
    defer wg.Done()
    for j := 0; j < 1000; j++ {
        increments <- struct{}{}
    }
    close(increments) // BUG: other goroutines are still sending!
}()
```
Sending on a closed channel causes a **panic**. Close the channel once from the coordinating goroutine, after `wg.Wait()` confirms all senders have finished.

### Using Unbuffered Channel with Many Senders
An unbuffered channel blocks the sender until the receiver is ready. With 1000 goroutines doing fine-grained operations, this creates excessive synchronization overhead. Use a buffered channel to allow senders to proceed without waiting for each receive.

### Not Considering Batching
Sending one signal per operation defeats the purpose of channels for high-frequency operations. Always ask: "Can I batch these messages?"

## Verify What You Learned

```bash
go run -race main.go
```

1. Confirm zero race warnings for both channel versions
2. Why is there no race on `counter` in `safeCounterChannel`?
3. When would you prefer the channel approach over a mutex?
4. Why is the batched version so much faster?

## What's Next
Continue to [05-fix-race-with-atomic](../05-fix-race-with-atomic/05-fix-race-with-atomic.md) to fix the same race using `sync/atomic` and compare all three approaches.

## Summary
- The channel approach eliminates races by giving data ownership to a single goroutine
- Worker goroutines communicate through channels instead of accessing shared memory
- "Don't communicate by sharing memory; share memory by communicating"
- `close(channel)` must happen after all senders are done, and before receiving the final result
- Batching reduces channel overhead: send one batch result instead of one signal per operation
- For simple counters, channels have more overhead than mutexes, but the pattern scales to complex state
- The race detector recognizes channel operations as synchronization points

## Reference
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Proverbs](https://go-proverbs.github.io/)
