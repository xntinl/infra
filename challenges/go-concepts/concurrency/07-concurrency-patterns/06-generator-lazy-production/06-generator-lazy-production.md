# 6. Generator: Lazy Production

<!--
difficulty: intermediate
concepts: [generator pattern, lazy evaluation, channel backpressure, producer-consumer]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, channel direction]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines and channels
- Familiarity with channel direction types (`<-chan`, `chan<-`)

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** generator functions that return receive-only channels
- **Explain** how channel backpressure drives lazy evaluation
- **Create** infinite generators that produce values on demand
- **Apply** the generator pattern as the entry point to pipelines

## Why Generators
A generator is a function that returns a channel and produces values in a background goroutine. The consumer drives the pace: if the consumer stops reading, the generator blocks on its send. This is lazy evaluation through backpressure -- values are produced only as fast as they are consumed.

Generators are the canonical way to create data sources in Go's channel-based concurrency model. They appear at the head of every pipeline and encapsulate the production logic behind a clean `<-chan T` interface. The consumer does not know (or care) whether the generator reads from a file, queries a database, computes values mathematically, or generates them randomly.

The key insight is that an unbuffered channel naturally synchronizes the producer and consumer. The producer only runs when the consumer is ready to receive. This makes generators memory-efficient even for infinite sequences -- only one value exists in flight at a time.

## Step 1 -- Finite Generator

Create a generator that produces a fixed sequence of values.

Edit `main.go` and implement the `rangeGen` function:

```go
func rangeGen(start, end int) <-chan int {
    out := make(chan int)
    go func() {
        for i := start; i <= end; i++ {
            out <- i
        }
        close(out)
    }()
    return out
}
```

The function returns immediately with the channel. Values are sent lazily as the consumer reads.

### Intermediate Verification
```bash
go run main.go
```
Expected:
```
Range [1,5]: 1 2 3 4 5
```

## Step 2 -- Infinite Generator

Create a generator for the Fibonacci sequence that never ends. The consumer decides how many values to take.

```go
func fibonacci() <-chan int {
    out := make(chan int)
    go func() {
        a, b := 0, 1
        for {
            out <- a
            a, b = b, a+b
        }
    }()
    return out
}
```

This goroutine runs forever, but it blocks on `out <- a` whenever the consumer is not reading. No CPU is wasted, no memory grows.

To consume only N values:

```go
func take(n int, in <-chan int) []int {
    result := make([]int, 0, n)
    for i := 0; i < n; i++ {
        v, ok := <-in
        if !ok {
            break
        }
        result = append(result, v)
    }
    return result
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected:
```
First 10 Fibonacci: [0 1 1 2 3 5 8 13 21 34]
```

**Warning:** The goroutine inside `fibonacci()` leaks after `take` returns -- it blocks forever on its send with no receiver. We address this in Step 3.

## Step 3 -- Generator with Done Channel

Fix the goroutine leak by adding a `done` channel for cancellation:

```go
func fibonacciWithDone(done <-chan struct{}) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        a, b := 0, 1
        for {
            select {
            case out <- a:
                a, b = b, a+b
            case <-done:
                return
            }
        }
    }()
    return out
}
```

The `select` statement lets the goroutine listen for both "consumer wants a value" and "consumer is done". Closing the `done` channel unblocks the `<-done` case and the goroutine exits cleanly.

### Intermediate Verification
```bash
go run main.go
```
Expected: same Fibonacci values, but no goroutine leak. Verify by printing a message when the generator exits:
```
First 10 Fibonacci (cancelable): [0 1 1 2 3 5 8 13 21 34]
  generator exited cleanly
```

## Step 4 -- Custom Generator Pattern

Build a generator that produces values from a custom function:

```go
func generateFrom(done <-chan struct{}, fn func(int) int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        i := 0
        for {
            select {
            case out <- fn(i):
                i++
            case <-done:
                return
            }
        }
    }()
    return out
}
```

This is a higher-order generator: it accepts a function that maps an index to a value.

### Intermediate Verification
```bash
go run main.go
```
Use it to generate squares and powers of 2:
```
Squares: [0 1 4 9 16 25 36 49]
Powers of 2: [1 2 4 8 16 32 64 128]
```

## Common Mistakes

### Goroutine Leak from Infinite Generators
**Wrong:**
```go
fib := fibonacci()
first5 := take(5, fib)
// goroutine inside fibonacci() is stuck on send forever
```
**What happens:** The goroutine never exits. In a long-running program, this accumulates leaked goroutines.

**Fix:** Always use a `done` channel (or `context.Context`) with infinite generators so you can signal the producer to stop.

### Buffering the Generator Channel
**Wrong:**
```go
out := make(chan int, 100) // pre-produces 100 values
```
**What happens:** The generator eagerly produces 100 values before any consumer reads. This wastes memory and defeats laziness.

**Fix:** Use unbuffered channels for true lazy evaluation. Only buffer when you have measured a performance need.

### Closing a Channel Twice
**Wrong:**
```go
go func() {
    for i := 0; i < 10; i++ {
        out <- i
    }
    close(out)
    // ... later, done channel triggers
    close(out) // panic: close of closed channel
}()
```
**Fix:** Use `defer close(out)` once, and structure the goroutine to have a single exit path.

## Verify What You Learned

Create a `primes` generator using the Sieve of Eratosthenes approach: a chain of goroutines, where each goroutine filters out multiples of a prime. Take the first 15 primes. Use a `done` channel to clean up all goroutines when done.

## What's Next
Continue to [07-or-channel-first-to-finish](../07-or-channel-first-to-finish/07-or-channel-first-to-finish.md) to learn how to race multiple goroutines and take the first result.

## Summary
- A generator is a function that returns `<-chan T` and produces values in a background goroutine
- Unbuffered channels provide natural backpressure: values are produced lazily on demand
- Infinite generators are safe because the goroutine blocks when no one is reading
- Always provide a cancellation mechanism (`done` channel or context) for infinite generators
- The `take` pattern consumes a fixed number of values from any generator
- Higher-order generators accept functions to customize value production

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs) -- generators and multiplexing
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Go Playground: Prime Sieve](https://go.dev/play/p/9U22NfrXeq) -- classic Go prime sieve example
