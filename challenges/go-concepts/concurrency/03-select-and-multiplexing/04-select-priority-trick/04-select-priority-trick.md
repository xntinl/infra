# 4. Select Priority Trick

<!--
difficulty: intermediate
concepts: [select, priority, nested-select, fairness, starvation]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [select-basics, select-with-default, channels, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-02 (select basics, select with default)
- Understanding of random case selection in `select`

## Learning Objectives
- **Demonstrate** that `select` picks randomly among ready cases
- **Implement** the nested-select trick to simulate priority
- **Analyze** the limitations and tradeoffs of this approach

## Why Priority

Go's `select` is fair by design: when multiple cases are ready, it picks one uniformly at random. This prevents starvation but creates a problem when some messages genuinely matter more than others. A shutdown signal should take precedence over a work item. A high-priority queue should drain before low-priority messages.

Go has no built-in priority `select`. The language designers intentionally avoided it because priority inversion and starvation are hard to reason about. But real systems need priority, so the community developed a pattern: the nested select trick. It is not perfect -- it trades fairness for priority in a best-effort manner -- but it is the standard idiom when one channel must be checked first.

Understanding this pattern also highlights a deeper truth: `select` is a building block, not a complete solution. Complex scheduling requires deliberate design above the language primitives.

## Step 1 -- Demonstrating Random Selection

First, prove that `select` is truly random when both channels are ready. Send many values on two channels and count which case gets selected.

```go
high := make(chan string, 100)
low := make(chan string, 100)

for i := 0; i < 100; i++ {
    high <- "high"
    low <- "low"
}

highCount, lowCount := 0, 0
for i := 0; i < 100; i++ {
    select {
    case <-high:
        highCount++
    case <-low:
        lowCount++
    }
}

fmt.Printf("high: %d, low: %d\n", highCount, lowCount)
```

You will see roughly 50/50 distribution. The "high" channel gets no special treatment.

### Intermediate Verification
Run multiple times. Both counts should hover around 50, varying by ~10. This confirms random selection.

## Step 2 -- The Nested Select Trick

To prioritize the high channel, check it first in an inner `select` with a `default` case. Only fall through to the outer `select` (which listens on both) if the high channel is empty.

```go
high := make(chan string, 100)
low := make(chan string, 100)

for i := 0; i < 100; i++ {
    high <- "high"
    low <- "low"
}

highCount, lowCount := 0, 0
for i := 0; i < 200; i++ {
    select {
    case <-high:
        highCount++
    default:
        select {
        case <-high:
            highCount++
        case <-low:
            lowCount++
        }
    }
}

fmt.Printf("high: %d, low: %d\n", highCount, lowCount)
```

The outer `select` first tries to receive from `high` only. If `high` is empty (hits `default`), the inner `select` listens on both channels. This drains the high-priority channel before touching the low-priority one.

### Intermediate Verification
Run the program. You should see approximately `high: 100, low: 100`, but critically, the high messages are consumed first. Add print statements inside the cases to verify ordering.

## Step 3 -- Priority with Continuous Producers

Apply the pattern to live goroutines that produce messages at different rates.

```go
high := make(chan string, 10)
low := make(chan string, 10)
done := make(chan struct{})

// High-priority producer
go func() {
    for i := 0; i < 5; i++ {
        high <- fmt.Sprintf("URGENT-%d", i)
        time.Sleep(50 * time.Millisecond)
    }
}()

// Low-priority producer
go func() {
    for i := 0; i < 20; i++ {
        low <- fmt.Sprintf("normal-%d", i)
        time.Sleep(10 * time.Millisecond)
    }
    close(done)
}()

for {
    select {
    case msg := <-high:
        fmt.Println("[HIGH]", msg)
    default:
        select {
        case msg := <-high:
            fmt.Println("[HIGH]", msg)
        case msg := <-low:
            fmt.Println("[LOW]", msg)
        case <-done:
            fmt.Println("all producers finished")
            return
        }
    }
}
```

The high-priority messages appear as soon as they arrive, even when low-priority messages are also available.

### Intermediate Verification
Run the program. Observe that URGENT messages are processed immediately, while normal messages fill the gaps.

## Common Mistakes

1. **Assuming perfect priority.** The nested select trick is best-effort. Between the outer `default` and the inner `select`, a high-priority message might arrive. The inner `select` then sees both channels ready and picks randomly. Priority is strongly biased, not absolute.

2. **Starving low-priority channels.** If the high-priority channel always has data, the low-priority channel is never read. This is by design for priority, but if the low-priority channel has a bounded buffer, its senders will block and potentially deadlock. Monitor queue depths.

3. **Nesting too deeply.** More than two priority levels with nested selects becomes unreadable. For complex priority, use a priority queue data structure protected by a mutex instead.

4. **Forgetting the done/quit channel in the inner select.** If `done` is only in the outer select, the goroutine can get stuck in the inner select waiting on low-priority messages after shutdown was signaled.

## Verify What You Learned

- [ ] Can you explain why a flat `select` cannot provide priority?
- [ ] Can you draw the flow of the nested select pattern?
- [ ] Can you describe a scenario where the priority trick fails (gives random selection)?
- [ ] Can you explain when you should use a priority queue + mutex instead?

## What's Next
In the next exercise, you will combine `select` with `for` loops to build continuous event loops -- the standard pattern for long-running goroutines.

## Summary
Go's `select` is intentionally fair. To simulate priority, use a nested select: the outer `select` tries the high-priority channel with a `default`, and the inner `select` listens on all channels. This drains high-priority messages first but is best-effort, not absolute. For more than two priority levels, prefer a priority queue with explicit locking.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Bryan Mills - Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
