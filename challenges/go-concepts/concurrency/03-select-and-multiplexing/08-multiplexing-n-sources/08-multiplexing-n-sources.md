# 8. Multiplexing N Sources

<!--
difficulty: advanced
concepts: [fan-in, merge, variadic-channels, WaitGroup, goroutine-per-channel, dynamic-multiplexing]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [select-basics, select-in-for-loop, done-channel-pattern, goroutines, WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01, 05, and 06 (select basics, for-select, done channel)
- Understanding of `sync.WaitGroup`
- Understanding of variadic functions

## Learning Objectives
- **Build** a merge function that combines N channels into one output channel
- **Use** goroutine-per-channel fan-in with proper cleanup via WaitGroup
- **Handle** source closure and output channel lifecycle correctly

## Why Multiplexing N Sources

Earlier exercises used `select` with a fixed number of channels. This works when the number of sources is known at compile time: two producers, three event streams, one quit signal. But many real systems have a dynamic number of sources: N microservice connections, a variable number of file watchers, or a pool of workers all reporting results.

You cannot write a `select` with a variable number of cases (Go's `select` requires cases to be lexically present at compile time). The solution is the fan-in pattern: spawn one goroutine per source channel, each forwarding its values to a single shared output channel. A `sync.WaitGroup` tracks when all source goroutines have finished, at which point the output channel is closed.

This is the general-purpose channel multiplexer. It appears in Go's standard patterns, in the `x/sync/errgroup` package, and in virtually every pipeline-based architecture. Mastering it gives you the ability to compose arbitrary channel topologies.

## Step 1 -- Merge Two Channels

Start with the simplest case: merge two channels into one.

```go
func merge(ch1, ch2 <-chan int) <-chan int {
    out := make(chan int)
    var wg sync.WaitGroup

    forward := func(ch <-chan int) {
        defer wg.Done()
        for val := range ch {
            out <- val
        }
    }

    wg.Add(2)
    go forward(ch1)
    go forward(ch2)

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

Usage:

```go
ch1 := make(chan int)
ch2 := make(chan int)

go func() {
    for i := 0; i < 5; i++ {
        ch1 <- i
        time.Sleep(30 * time.Millisecond)
    }
    close(ch1)
}()

go func() {
    for i := 100; i < 105; i++ {
        ch2 <- i
        time.Sleep(50 * time.Millisecond)
    }
    close(ch2)
}()

for val := range merge(ch1, ch2) {
    fmt.Println("merged:", val)
}
fmt.Println("all sources closed")
```

Each source gets its own goroutine that forwards values to `out`. When a source closes, `range` exits and `wg.Done()` is called. After all sources finish, the WaitGroup goroutine closes `out`, which terminates the consumer's `range`.

### Intermediate Verification
Run the program. You should see 10 values (0-4 and 100-104) in interleaved order, then "all sources closed".

## Step 2 -- Generalize to N Channels

Replace the two-channel merge with a variadic version that accepts any number of channels.

```go
func mergeN(channels ...<-chan int) <-chan int {
    out := make(chan int)
    var wg sync.WaitGroup

    forward := func(ch <-chan int) {
        defer wg.Done()
        for val := range ch {
            out <- val
        }
    }

    wg.Add(len(channels))
    for _, ch := range channels {
        go forward(ch)
    }

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

Usage with dynamic number of sources:

```go
n := 4
sources := make([]<-chan int, n)

for i := 0; i < n; i++ {
    ch := make(chan int)
    sources[i] = ch
    go func(id int, c chan<- int) {
        for j := 0; j < 3; j++ {
            c <- id*100 + j
            time.Sleep(time.Duration(20*(id+1)) * time.Millisecond)
        }
        close(c)
    }(i, ch)
}

for val := range mergeN(sources...) {
    fmt.Printf("received: %d\n", val)
}
fmt.Println("all done")
```

The pattern is identical to the two-channel version. The only change is iterating over the variadic slice instead of hardcoding two goroutines.

### Intermediate Verification
Run the program. You should see 12 values (4 sources x 3 values each) in interleaved order, then "all done".

## Step 3 -- Merge with Cancellation

Add a done channel to the merge function so the consumer can cancel all forwarding goroutines without waiting for sources to close.

```go
func mergeWithDone(done <-chan struct{}, channels ...<-chan int) <-chan int {
    out := make(chan int)
    var wg sync.WaitGroup

    forward := func(ch <-chan int) {
        defer wg.Done()
        for val := range ch {
            select {
            case <-done:
                return
            case out <- val:
            }
        }
    }

    wg.Add(len(channels))
    for _, ch := range channels {
        go forward(ch)
    }

    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

Usage:

```go
done := make(chan struct{})

// Create sources that produce indefinitely
sources := make([]<-chan int, 3)
for i := 0; i < 3; i++ {
    ch := make(chan int)
    sources[i] = ch
    go func(id int, c chan<- int) {
        val := 0
        for {
            select {
            case <-done:
                close(c)
                return
            case c <- val:
                val++
                time.Sleep(50 * time.Millisecond)
            }
        }
    }(i, ch)
}

merged := mergeWithDone(done, sources...)

// Consume 10 values, then cancel
for i := 0; i < 10; i++ {
    fmt.Println("value:", <-merged)
}

close(done)
// Drain remaining values to unblock goroutines
for range merged {
}
fmt.Println("cancelled and cleaned up")
```

The forward goroutines check `done` on every send to `out`. When `done` is closed, they exit immediately. The drain loop after `close(done)` consumes any values that were in flight before the forwarders noticed the cancellation.

### Intermediate Verification
Run the program. You should see exactly 10 values, then "cancelled and cleaned up". No goroutine leaks.

## Common Mistakes

1. **Closing the output channel in forward goroutines.** Only one goroutine should close `out`, and only after all forwarders have finished. This is the WaitGroup goroutine's job. If a forwarder closes `out`, other forwarders will panic when they try to send.

2. **Not closing source channels.** The forwarder uses `range`, which blocks until the source closes. If a source never closes and there is no done channel, the forwarder goroutine leaks forever.

3. **Capturing the loop variable in Go < 1.22.** In Go versions before 1.22, `for _, ch := range channels { go forward(ch) }` captures the loop variable correctly only because `ch` is passed as a function argument. If you used a closure capturing `ch` directly in older Go versions, all goroutines would share the last value.

4. **Forgetting to drain after cancellation.** If forwarding goroutines sent values to `out` before noticing `done`, those values sit in the channel. Without draining, the goroutines block on the send forever.

## Verify What You Learned

- [ ] Can you explain why each source needs its own forwarding goroutine?
- [ ] Can you describe the role of the WaitGroup goroutine and why it must be separate?
- [ ] Can you explain why draining is necessary after cancellation?
- [ ] Can you extend this to merge channels of different types using generics?

## What's Next
You have completed the select and multiplexing section. The next section covers sync primitives (`sync.Mutex`, `sync.RWMutex`, `sync.Once`, `sync.Pool`) for shared-state concurrency.

## Summary
Multiplexing N channels into one uses the fan-in pattern: one goroutine per source forwards values to a shared output channel. A `sync.WaitGroup` tracks forwarders and a separate goroutine closes the output channel when all forwarders are done. Adding a done channel enables cancellation. This pattern is the general-purpose solution for dynamic channel composition and appears throughout production Go code.

## Reference
- [Go Concurrency Patterns: Fan-in](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
- [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup)
