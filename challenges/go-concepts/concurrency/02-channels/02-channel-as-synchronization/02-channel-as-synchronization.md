# 2. Channel as Synchronization

<!--
difficulty: basic
concepts: [channels, synchronization, done-channel, signaling, goroutine-coordination]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, unbuffered-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (unbuffered channel basics)
- Understanding of goroutine lifecycle

## Learning Objectives
After completing this exercise, you will be able to:
- **Replace** fragile `time.Sleep` synchronization with channel-based signaling
- **Implement** the done-channel pattern to wait for goroutine completion
- **Explain** why channel synchronization is deterministic while sleep is not

## Why Channel Synchronization

When you first learn goroutines, `time.Sleep` seems like a quick way to "wait" for goroutines to finish. But `time.Sleep` is a guess — you're betting that the goroutine finishes within the sleep duration. On a slow machine, under heavy load, or with network calls, that bet fails silently. Your program exits before the goroutine finishes, and you lose results with no error message.

Channels give you a guarantee instead of a guess. When you receive from a done channel, you know the goroutine has completed because it sent the signal. It doesn't matter if the work took 1ms or 10 seconds — the receiver waits exactly as long as needed.

This pattern is so fundamental that it appears in virtually every production Go program. It's the building block for more sophisticated patterns like fan-out/fan-in, pipelines, and graceful shutdown.

## Step 1 -- The Fragile Sleep Version

Start with code that uses `time.Sleep` to wait for goroutines. Observe how it breaks when the work takes longer than expected.

```go
func worker(id int) {
    fmt.Printf("Worker %d: starting\n", id)
    // Simulate variable-length work
    time.Sleep(time.Duration(id*100) * time.Millisecond)
    fmt.Printf("Worker %d: done\n", id)
}

func main() {
    for i := 1; i <= 3; i++ {
        go worker(i)
    }
    // Hope that 200ms is enough... it's not for worker 3
    time.Sleep(200 * time.Millisecond)
    fmt.Println("main: exiting")
}
```

Worker 3 needs 300ms but main only waits 200ms. Worker 3's completion message is lost.

### Intermediate Verification
```bash
go run main.go
# You'll see worker 3 is missing its "done" message
```

## Step 2 -- Convert to Done Channel

Replace `time.Sleep` with a done channel. Each goroutine signals completion by sending on the channel.

```go
func workerWithSignal(id int, done chan bool) {
    fmt.Printf("Worker %d: starting\n", id)
    time.Sleep(time.Duration(id*100) * time.Millisecond)
    fmt.Printf("Worker %d: done\n", id)
    done <- true // signal completion
}
```

In `main`, receive from the done channel once for each goroutine launched. This guarantees all workers finish before main exits.

### Intermediate Verification
```bash
go run main.go
# All three workers should print "done" messages
# main exits only after all workers complete
```

## Step 3 -- Signal Without Data: struct{}

When a channel is used purely for signaling (the value itself doesn't matter), use `chan struct{}` instead of `chan bool`. It communicates intent and uses zero memory per value.

```go
done := make(chan struct{})

go func() {
    // ... do work ...
    done <- struct{}{} // signal: "I'm done"
}()

<-done // wait for signal
```

Convert your step 2 solution to use `chan struct{}`.

### Intermediate Verification
```bash
go run main.go
# Same behavior as step 2, but with clearer intent
```

## Step 4 -- Waiting for N Goroutines

When you launch N goroutines, you need N receives to wait for all of them. Implement a function that launches N workers and waits for all of them using a single done channel.

```go
func launchWorkers(n int) {
    done := make(chan struct{})

    for i := 1; i <= n; i++ {
        go func(id int) {
            fmt.Printf("Worker %d: processing\n", id)
            time.Sleep(time.Duration(id*50) * time.Millisecond)
            fmt.Printf("Worker %d: finished\n", id)
            done <- struct{}{}
        }(i)
    }

    // Wait for all N goroutines
    for i := 0; i < n; i++ {
        <-done
    }
    fmt.Println("All workers completed")
}
```

### Intermediate Verification
```bash
go run main.go
# All workers finish, then "All workers completed" prints last
```

## Common Mistakes

### Mismatched Send/Receive Count
**Wrong:**
```go
for i := 0; i < 5; i++ {
    go func() { done <- struct{}{} }()
}
for i := 0; i < 3; i++ { // only waiting for 3!
    <-done
}
```
**What happens:** Main exits while 2 goroutines are still running (or trying to send). You lose work.
**Fix:** Always match the number of receives to the number of goroutines.

### Sending Before the Work Is Done
**Wrong:**
```go
go func() {
    done <- struct{}{} // signal sent before work!
    doExpensiveWork()
}()
<-done
// main continues, but the goroutine is still working
```
**What happens:** The signal arrives before the work completes. Main proceeds with incomplete results.
**Fix:** Always send the done signal as the last operation in the goroutine.

## Verify What You Learned

Implement the `reliableProcessor` function in `main.go`: launch 5 goroutines, each simulating variable-length work (use their ID to vary sleep duration). Each goroutine should print what it's working on and when it finishes. Main must wait for ALL goroutines to complete using channel synchronization (no `time.Sleep`), then print the total elapsed time. Verify that the total time is roughly equal to the slowest worker, not the sum of all workers.

## What's Next
Continue to [03-buffered-channels](../03-buffered-channels/03-buffered-channels.md) to learn how buffered channels decouple senders from receivers.

## Summary
- `time.Sleep` for synchronization is fragile — it guesses instead of guaranteeing
- Done channels provide deterministic synchronization: receive blocks until the sender signals
- Use `chan struct{}` for pure signaling where the value doesn't matter
- To wait for N goroutines, perform N receives on the done channel
- Always send the done signal as the last operation in the goroutine

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Go Blog: Go Concurrency Patterns](https://go.dev/blog/concurrency-patterns)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
