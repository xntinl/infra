# 2. Select with Default

<!--
difficulty: basic
concepts: [select, default-case, non-blocking-operations, polling]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [select-basics, channels, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (select basics)
- Understanding of blocking vs. non-blocking operations

## Learning Objectives
- **Use** the `default` case to make channel operations non-blocking
- **Implement** a non-blocking receive and a non-blocking send
- **Build** a polling loop using `select` with `default`

## Why Default in Select

A plain `select` blocks until one of its channel operations can proceed. This is usually what you want, but sometimes blocking is unacceptable. You might need to check a channel without waiting, attempt a send that should be dropped if the receiver is not ready, or do useful work between channel checks.

The `default` case transforms `select` from a blocking multiplexer into a non-blocking probe. When present, `default` executes immediately if no other case is ready. This gives you a try-operation: "receive if there is something, otherwise continue."

This pattern appears in rate limiters (try to acquire a token, skip if none available), logging pipelines (send the log entry, drop it if the buffer is full), and polling loops where the goroutine must remain responsive.

## Step 1 -- Non-Blocking Receive

Try to receive from a channel without blocking. If nothing is available, the `default` case runs.

```go
ch := make(chan string, 1)

// Channel is empty -- select will hit default
select {
case msg := <-ch:
    fmt.Println("received:", msg)
default:
    fmt.Println("no message available")
}

// Now put something in the channel
ch <- "hello"

// Channel has a value -- select will receive it
select {
case msg := <-ch:
    fmt.Println("received:", msg)
default:
    fmt.Println("no message available")
}
```

The first `select` hits `default` because the channel is empty. The second `select` receives "hello" because the buffer contains a value.

### Intermediate Verification
Run the program. You should see "no message available" followed by "received: hello".

## Step 2 -- Non-Blocking Send

Attempt to send on a channel without blocking. If the channel's buffer is full (or no receiver is waiting), the `default` case runs and the value is dropped.

```go
ch := make(chan int, 1)

// First send succeeds -- buffer has space
select {
case ch <- 1:
    fmt.Println("sent 1")
default:
    fmt.Println("channel full, dropped")
}

// Second send hits default -- buffer is full
select {
case ch <- 2:
    fmt.Println("sent 2")
default:
    fmt.Println("channel full, dropped")
}

fmt.Println("buffered value:", <-ch)
```

This is the "fire and forget" pattern. It is useful when dropping a message is acceptable, such as non-critical metrics or overflow logs.

### Intermediate Verification
You should see "sent 1", "channel full, dropped", and "buffered value: 1".

## Step 3 -- Polling Pattern

Combine `select` + `default` inside a loop to poll a channel while doing other work. This creates a cooperative multitasking loop.

```go
messages := make(chan string, 1)

go func() {
    time.Sleep(200 * time.Millisecond)
    messages <- "data ready"
}()

for i := 0; i < 5; i++ {
    select {
    case msg := <-messages:
        fmt.Println("got:", msg)
        return
    default:
        fmt.Println("no message yet, doing other work...")
        time.Sleep(100 * time.Millisecond)
    }
}
fmt.Println("gave up waiting")
```

Each loop iteration checks the channel. If nothing is there, it does other work (simulated by sleep) and checks again. After a few iterations the goroutine delivers data.

### Intermediate Verification
You should see 2-3 "no message yet" lines followed by "got: data ready". The exact count depends on scheduling.

## Common Mistakes

1. **Using default when you should block.** Adding `default` to every `select` turns blocking waits into busy loops that burn CPU. Only use `default` when you genuinely need non-blocking behavior.

2. **Polling without sleep or work.** A `for { select { default: } }` with no work in the default case is a tight spin loop. It will consume 100% of a CPU core. Always include meaningful work or a small sleep.

3. **Confusing "non-blocking" with "instant".** The `default` case makes the `select` non-blocking, but the goroutine still takes time to execute the default body. It is not a zero-cost operation.

## Verify What You Learned

- [ ] Can you explain the difference between `select` with and without `default`?
- [ ] Can you describe a scenario where a non-blocking send is the right choice?
- [ ] Can you identify the risk of using `default` inside a tight loop?

## What's Next
In the next exercise, you will learn how to use `time.After` and `time.NewTimer` to add timeout behavior to `select` statements.

## Summary
The `default` case in `select` makes channel operations non-blocking. A non-blocking receive checks a channel and continues immediately if empty. A non-blocking send drops the value if the channel is full. Combined with a loop, `select` + `default` creates a polling pattern. Use it deliberately -- unnecessary `default` cases turn efficient blocking into wasteful spinning.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Go by Example: Non-Blocking Channel Operations](https://gobyexample.com/non-blocking-channel-operations)
