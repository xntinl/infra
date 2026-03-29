/# 1. Launching Goroutines

<!--
difficulty: basic
concepts: [go keyword, concurrent execution, time.Sleep, anonymous goroutines]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [Go basics, functions, closures]
-->

## Prerequisites
- Go 1.22+ installed
- Familiarity with Go functions and closures
- Basic understanding of sequential program execution

## Learning Objectives
After completing this exercise, you will be able to:
- **Launch** concurrent goroutines using the `go` keyword
- **Distinguish** between sequential and concurrent execution
- **Create** both named and anonymous goroutines
- **Pass** arguments safely to goroutines

## Why Goroutines
Goroutines are the fundamental unit of concurrency in Go. Unlike threads in most languages, goroutines are extraordinarily cheap to create, use minimal memory (starting at just a few kilobytes of stack), and are multiplexed onto a small number of OS threads by the Go runtime scheduler.

The `go` keyword is the gateway to concurrent programming in Go. Every concurrent Go program starts here: by placing `go` before a function call, you tell the runtime to execute that function independently, without waiting for it to finish. Understanding how goroutines launch, how they interleave with `main`, and how to pass data to them safely is the bedrock upon which all other concurrency patterns are built.

A critical subtlety is that `main` itself runs in a goroutine. When `main` returns, all other goroutines are terminated immediately, regardless of whether they have finished. This means you must explicitly wait for goroutines to complete -- a theme that will recur throughout this series.

## Step 1 -- Sequential vs Concurrent Execution

Start by observing the difference between calling functions sequentially and launching them as goroutines.

Edit `main.go` and replace the `sequentialWork` function body:

```go
func sequentialWork() {
    fmt.Println("=== Sequential Execution ===")
    start := time.Now()

    printNumbers("A")
    printNumbers("B")
    printNumbers("C")

    fmt.Printf("Sequential took: %v\n\n", time.Since(start))
}
```

Each call to `printNumbers` must finish before the next one starts.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order is deterministic):
```
=== Sequential Execution ===
A-0 A-1 A-2 A-3 A-4
B-0 B-1 B-2 B-3 B-4
C-0 C-1 C-2 C-3 C-4
Sequential took: ~300ms
```

## Step 2 -- Launching Goroutines with `go`

Now implement `concurrentWork` to run the same functions as goroutines:

```go
func concurrentWork() {
    fmt.Println("=== Concurrent Execution ===")
    start := time.Now()

    go printNumbers("A")
    go printNumbers("B")
    go printNumbers("C")

    // Without this sleep, main exits before goroutines finish
    time.Sleep(200 * time.Millisecond)

    fmt.Printf("Concurrent took: %v\n\n", time.Since(start))
}
```

Notice the output is interleaved -- the order is non-deterministic. All three goroutines run concurrently.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order will vary between runs):
```
=== Concurrent Execution ===
A-0 C-0 B-0 A-1 B-1 C-1 ...
Concurrent took: ~200ms
```

## Step 3 -- Anonymous Goroutines

Implement `anonymousGoroutines` using inline anonymous functions:

```go
func anonymousGoroutines() {
    fmt.Println("=== Anonymous Goroutines ===")

    go func() {
        fmt.Println("Hello from anonymous goroutine 1")
    }()

    go func(msg string) {
        fmt.Println(msg)
    }("Hello from anonymous goroutine 2")

    time.Sleep(50 * time.Millisecond)
    fmt.Println()
}
```

Note the `()` at the end -- anonymous goroutines must be immediately invoked.

### Intermediate Verification
```bash
go run main.go
```
Both messages should print (in either order).

## Step 4 -- Passing Arguments Safely

Implement `safeArgumentPassing` to demonstrate the correct way to pass loop variables to goroutines:

```go
func safeArgumentPassing() {
    fmt.Println("=== Safe Argument Passing ===")

    // CORRECT: pass the loop variable as a function argument
    for i := 0; i < 5; i++ {
        go func(n int) {
            fmt.Printf("goroutine received: %d\n", n)
        }(i) // i is copied into n at launch time
    }

    time.Sleep(50 * time.Millisecond)
    fmt.Println()
}
```

Each goroutine receives its own copy of `i` at the moment it is launched.

### Intermediate Verification
```bash
go run main.go
```
You should see all values 0-4 printed (in any order), each appearing exactly once.

## Common Mistakes

### Capturing Loop Variables by Reference
**Wrong:**
```go
for i := 0; i < 5; i++ {
    go func() {
        fmt.Println(i) // captures variable i, not its value
    }()
}
time.Sleep(50 * time.Millisecond)
```
**What happens:** All goroutines likely print `5` because they share the same `i`, which has reached `5` by the time they execute.

**Fix:**
```go
for i := 0; i < 5; i++ {
    go func(n int) {
        fmt.Println(n) // n is a copy, independent per goroutine
    }(i)
}
time.Sleep(50 * time.Millisecond)
```

> **Note:** Starting in Go 1.22, the loop variable semantics changed so that each iteration gets its own variable. However, passing arguments explicitly remains the idiomatic and clearest approach.

### Forgetting to Wait for Goroutines
**Wrong:**
```go
func main() {
    go fmt.Println("hello")
    // main exits immediately -- goroutine never runs
}
```
**What happens:** The program exits before the goroutine has a chance to execute.

**Fix:** Use `time.Sleep` (temporary) or proper synchronization like `sync.WaitGroup` (which you will learn in later exercises).

### Launching a Goroutine on a Method Call Result
**Wrong:**
```go
go result := compute() // syntax error: go does not return values
```
**What happens:** Compilation error. The `go` keyword starts a function call concurrently; it cannot capture return values.

**Fix:** Use a closure that writes to a shared variable or a channel:
```go
var result int
go func() {
    result = compute()
}()
```

## Verify What You Learned

Combine all concepts: create a function `fanOut` that launches N goroutines, where each goroutine:
1. Receives its own index as an argument
2. Prints a message identifying itself and the total count
3. Simulates work with `time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)`
4. Prints a completion message

Call `fanOut(10)` and observe the non-deterministic interleaving.

## What's Next
Continue to [02-goroutine-vs-os-thread](../02-goroutine-vs-os-thread/02-goroutine-vs-os-thread.md) to understand why goroutines are so much cheaper than OS threads.

## Summary
- The `go` keyword launches a function call as an independent goroutine
- `main` is itself a goroutine; when it exits, all other goroutines are killed
- Anonymous goroutines must be immediately invoked with `()`
- Always pass loop variables as function arguments to avoid shared-variable bugs
- Goroutine execution order is non-deterministic
- `time.Sleep` is a temporary synchronization hack; proper patterns come later

## Reference
- [Go Tour: Goroutines](https://go.dev/tour/concurrency/1)
- [Effective Go: Goroutines](https://go.dev/doc/effective_go#goroutines)
- [Go Spec: Go Statements](https://go.dev/ref/spec#Go_statements)
