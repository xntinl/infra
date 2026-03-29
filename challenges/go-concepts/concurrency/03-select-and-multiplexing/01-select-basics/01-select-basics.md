# 1. Select Basics

<!--
difficulty: basic
concepts: [select, channels, goroutines, multiplexing]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [goroutines, unbuffered-channels, buffered-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines (section 01)
- Understanding of channels: sending, receiving, blocking behavior (section 02)

## Learning Objectives
- **Explain** what the `select` statement does and why it exists
- **Use** `select` to listen on multiple channels simultaneously
- **Observe** the random selection behavior when multiple channels are ready

## Why Select

When a goroutine needs to communicate with a single channel, a plain send or receive is enough. But real programs rarely have just one communication path. A web server might wait for an incoming request, a timeout signal, and a shutdown notification all at once. Without `select`, the goroutine would block on one channel and miss messages arriving on the others.

The `select` statement is Go's multiplexer for channel operations. It blocks until one of its cases can proceed, then executes that case. If multiple cases are ready simultaneously, it picks one at random with uniform probability. This randomness is intentional: it prevents starvation and ensures no single channel can monopolize the goroutine's attention.

Think of `select` as a `switch` statement for channels. Where `switch` evaluates values, `select` evaluates communication readiness. It is the foundation for almost every concurrent pattern in Go: timeouts, cancellation, fan-in, heartbeats, and priority handling.

## Step 1 -- Two Channels, One Listener

Create two channels and two goroutines that send values at different speeds. Use `select` to receive from whichever channel has data ready first.

```go
fast := make(chan string)
slow := make(chan string)

go func() {
    time.Sleep(100 * time.Millisecond)
    fast <- "fast message"
}()

go func() {
    time.Sleep(300 * time.Millisecond)
    slow <- "slow message"
}()

select {
case msg := <-fast:
    fmt.Println("received:", msg)
case msg := <-slow:
    fmt.Println("received:", msg)
}
```

The `select` blocks until one of the two channels delivers a value. Since `fast` sends after 100ms and `slow` after 300ms, the fast case wins. The slow goroutine's message is never received because the program exits after the first `select` completes.

### Intermediate Verification
Run your program. You should see `received: fast message` every time. Try swapping the sleep durations and confirm the output changes.

## Step 2 -- Observing Random Selection

When both channels are ready at the same moment, `select` picks at random. To observe this, remove the sleep calls so both goroutines send immediately on buffered channels.

```go
ch1 := make(chan string, 1)
ch2 := make(chan string, 1)

ch1 <- "from ch1"
ch2 <- "from ch2"

select {
case msg := <-ch1:
    fmt.Println("selected:", msg)
case msg := <-ch2:
    fmt.Println("selected:", msg)
}
```

Since both channels already have a value buffered, both cases are ready. The runtime picks one uniformly at random.

### Intermediate Verification
Run the program 10+ times. You should see roughly half `from ch1` and half `from ch2`. If you always see the same result, increase your sample size -- randomness requires enough trials to manifest.

## Step 3 -- Multiple Select Rounds

Use a loop to drain both channels and confirm that `select` handles each message eventually.

```go
ch1 := make(chan string, 1)
ch2 := make(chan string, 1)

ch1 <- "alpha"
ch2 <- "beta"

for i := 0; i < 2; i++ {
    select {
    case msg := <-ch1:
        fmt.Println("round", i, "got:", msg)
    case msg := <-ch2:
        fmt.Println("round", i, "got:", msg)
    }
}
```

The first `select` picks one channel at random. The second `select` has only one channel with data left, so it picks that one deterministically.

### Intermediate Verification
Run multiple times. The order of "alpha" and "beta" should vary, but both always appear.

## Common Mistakes

1. **Assuming case order matters.** Unlike `switch`, the position of cases in `select` has no effect on priority. Go's runtime uses a pseudo-random shuffle to guarantee fairness.

2. **Forgetting that select blocks.** If no case is ready and there is no `default`, the goroutine blocks forever. This is a common source of deadlocks in programs without timeouts or done channels.

3. **Using select with a single case.** A `select` with one case is equivalent to a plain channel operation. It compiles but adds no value and obscures intent.

## Verify What You Learned

- [ ] Can you explain when `select` blocks vs. proceeds immediately?
- [ ] Can you describe what happens when multiple cases are ready?
- [ ] Can you write a `select` that listens on 3+ channels?

## What's Next
In the next exercise, you will learn about the `default` case in `select`, which enables non-blocking channel operations.

## Summary
The `select` statement multiplexes across multiple channel operations. It blocks until at least one case is ready, then executes it. When multiple cases are ready simultaneously, the runtime picks one uniformly at random, preventing starvation. This is the fundamental building block for all advanced concurrency patterns in Go.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Effective Go: Multiplexing](https://go.dev/doc/effective_go#multiplexing)
- [A Tour of Go: Select](https://go.dev/tour/concurrency/5)
