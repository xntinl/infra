# 3. Select with Timeout

<!--
difficulty: intermediate
concepts: [select, timeout, time.After, time.NewTimer, timer-cleanup]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [select-basics, select-with-default, channels, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-02 (select basics, select with default)
- Familiarity with `time` package basics

## Learning Objectives
- **Implement** a timeout on a channel operation using `time.After`
- **Explain** the resource leak caused by `time.After` inside loops
- **Use** `time.NewTimer` with proper cleanup for safe timeouts in loops

## Why Timeouts

Blocking forever on a channel is rarely acceptable in production systems. Network calls fail, downstream services hang, and goroutines can deadlock. Timeouts provide a safety valve: if the expected event does not happen within a deadline, the program recovers gracefully instead of hanging indefinitely.

Go does not have a built-in timeout keyword. Instead, it composes timeouts from two primitives: `time.After` (which returns a channel that receives after a delay) and `select` (which can listen on that channel alongside the main work channel). This composability is elegant but has a subtle trap: `time.After` allocates a timer that is not garbage collected until it fires. In a loop, this creates a leak.

The `time.NewTimer` type offers explicit control. You can stop it, reset it, and drain it. In any code path where timeouts happen repeatedly, `time.NewTimer` is the safe choice.

## Step 1 -- Basic Timeout with time.After

Use `time.After` inside a `select` to set a deadline on a channel receive.

```go
result := make(chan string)

go func() {
    time.Sleep(2 * time.Second) // Simulate slow work
    result <- "done"
}()

select {
case res := <-result:
    fmt.Println("result:", res)
case <-time.After(500 * time.Millisecond):
    fmt.Println("timeout: operation took too long")
}
```

`time.After(500ms)` returns a `<-chan time.Time` that receives a value after 500ms. The goroutine takes 2 seconds, so the timeout case wins.

### Intermediate Verification
Run the program. You should see "timeout: operation took too long" after ~500ms. Change the goroutine sleep to 100ms and confirm you get the result instead.

## Step 2 -- The time.After Leak in Loops

When `time.After` is called inside a loop, each iteration creates a new timer. If the main case resolves before the timeout, the timer still exists in memory until it fires.

```go
ch := make(chan int, 1)

go func() {
    for i := 0; i < 1000; i++ {
        ch <- i
        time.Sleep(1 * time.Millisecond)
    }
    close(ch)
}()

for val := range ch {
    select {
    case <-time.After(1 * time.Second): // LEAK: 1000 timers allocated
        fmt.Println("timeout")
        return
    default:
        fmt.Println("received:", val)
    }
}
```

This code creates ~1000 timers, each scheduled to fire after 1 second. They all stay in memory until they expire. In a high-throughput loop, this wastes significant memory and puts pressure on the runtime's timer heap.

### Intermediate Verification
This step is observational. Understand the pattern -- you will fix it in Step 3.

## Step 3 -- Safe Timeouts with time.NewTimer

Replace `time.After` with `time.NewTimer`. Stop the timer when it is no longer needed, and reset it between iterations.

```go
ch := make(chan int, 1)

go func() {
    for i := 0; i < 10; i++ {
        ch <- i
        time.Sleep(50 * time.Millisecond)
    }
    close(ch)
}()

timeout := time.NewTimer(500 * time.Millisecond)
defer timeout.Stop() // Always stop when done

for {
    // Reset the timer at the start of each iteration
    if !timeout.Stop() {
        select {
        case <-timeout.C:
        default:
        }
    }
    timeout.Reset(500 * time.Millisecond)

    select {
    case val, ok := <-ch:
        if !ok {
            fmt.Println("channel closed, all received")
            return
        }
        fmt.Println("received:", val)
    case <-timeout.C:
        fmt.Println("timeout: no data for 500ms")
        return
    }
}
```

Key points:
- `timeout.Stop()` returns false if the timer already fired. In that case, drain the channel to prevent a stale fire on the next iteration.
- `timeout.Reset()` rearms the timer for the next loop iteration.
- `defer timeout.Stop()` ensures cleanup if the function exits via any path.

### Intermediate Verification
Run the program. You should see values 0-9 printed, followed by "channel closed, all received". Change the producer sleep to 600ms and verify the timeout fires after the first value.

## Common Mistakes

1. **Using `time.After` in hot loops.** Every call allocates a new timer that lives until it fires. In a loop processing thousands of events per second, this leaks memory rapidly. Always use `time.NewTimer` in loops.

2. **Not draining the timer channel before Reset.** If the timer fired between `Stop()` and `Reset()`, the channel has a pending value. The next `select` will immediately see the old timeout. The drain pattern (`select { case <-t.C: default: }`) prevents this.

3. **Forgetting defer Stop().** If you return from the function without stopping the timer, it remains in the runtime's timer heap until it fires. This is a minor leak for short timers but compounds in long-running servers.

4. **Using too-short timeouts in tests.** CI environments are slower than local machines. Use generous timeouts in tests and tight ones only when testing the timeout itself.

## Verify What You Learned

- [ ] Can you explain why `time.After` in a loop leaks timers?
- [ ] Can you write the drain-before-reset pattern from memory?
- [ ] Can you describe when `time.After` is safe to use (hint: outside loops, one-shot operations)?

## What's Next
In the next exercise, you will learn how to simulate priority in `select` using nested selects, since Go's `select` has no built-in priority mechanism.

## Summary
`time.After` is convenient for one-shot timeouts: it returns a channel that fires after a delay. Inside loops, it leaks because each call creates a timer that lives until it fires. `time.NewTimer` provides explicit control: you can `Stop()`, drain, and `Reset()` a single timer across iterations. Always use `time.NewTimer` for repeated timeout operations, and always call `Stop()` on timers you no longer need.

## Reference
- [time.After](https://pkg.go.dev/time#After)
- [time.NewTimer](https://pkg.go.dev/time#NewTimer)
- [time.Timer.Reset](https://pkg.go.dev/time#Timer.Reset)
- [Go Wiki: Timer Reset](https://go.dev/wiki/Go-1.23-release-notes)
