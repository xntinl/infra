# 11. Defer Stacking and Resource Cleanup

<!--
difficulty: intermediate
concepts: [defer, defer-stack, resource-cleanup, lifo-order, defer-with-closures]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [function-declaration, closures, error-handling-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

- **Apply** `defer` for reliable resource cleanup in Go
- **Analyze** the LIFO (last-in, first-out) execution order of deferred calls
- **Identify** common patterns: file closing, mutex unlocking, and timing

## Why Defer

The `defer` statement schedules a function call to run when the enclosing function returns. This guarantees cleanup happens regardless of how the function exits — whether by reaching the end, returning early, or panicking.

Without `defer`, cleanup code must appear on every return path. If a function has multiple return statements, you must remember to close files, unlock mutexes, or release resources at each one. Missing even one path creates a resource leak. `defer` solves this by letting you place cleanup next to acquisition, and Go guarantees it will execute.

Deferred calls execute in LIFO (last-in, first-out) order, which naturally unwinds resources in the reverse order they were acquired — just like a stack.

## Step 1 — Basic Defer

```go
package main

import "fmt"

func main() {
    fmt.Println("start")
    defer fmt.Println("deferred")
    fmt.Println("end")
}
```

The deferred call runs after the function body completes but before the function actually returns to its caller.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
start
end
deferred
```

## Step 2 — LIFO Ordering

Multiple defers execute in reverse order (like a stack):

```go
package main

import "fmt"

func main() {
    for i := range 5 {
        defer fmt.Println("defer:", i)
    }
    fmt.Println("main body done")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
main body done
defer: 4
defer: 3
defer: 2
defer: 1
defer: 0
```

## Step 3 — File Cleanup Pattern

The most common use of `defer` is closing files immediately after opening them:

```go
package main

import (
    "fmt"
    "os"
)

func readFile(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil {
        return "", err
    }
    defer f.Close() // guaranteed cleanup

    buf := make([]byte, 1024)
    n, err := f.Read(buf)
    if err != nil {
        return "", err // f.Close() still runs
    }
    return string(buf[:n]), nil
}

func main() {
    // Create a temp file to demonstrate
    f, err := os.CreateTemp("", "demo-*.txt")
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    f.Write([]byte("Hello from defer!"))
    name := f.Name()
    f.Close()

    content, err := readFile(name)
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Println("Content:", content)

    os.Remove(name)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Content: Hello from defer!
```

## Step 4 — Mutex Unlock Pattern

```go
package main

import (
    "fmt"
    "sync"
)

type SafeCounter struct {
    mu sync.Mutex
    v  map[string]int
}

func (c *SafeCounter) Inc(key string) {
    c.mu.Lock()
    defer c.mu.Unlock() // unlocks no matter what

    c.v[key]++
}

func (c *SafeCounter) Value(key string) int {
    c.mu.Lock()
    defer c.mu.Unlock()

    return c.v[key]
}

func main() {
    counter := SafeCounter{v: make(map[string]int)}

    var wg sync.WaitGroup
    for range 1000 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            counter.Inc("key")
        }()
    }
    wg.Wait()

    fmt.Println("Count:", counter.Value("key"))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Count: 1000
```

## Step 5 — Defer with Closures and Argument Evaluation

Arguments to deferred function calls are evaluated immediately, but closures capture variables by reference:

```go
package main

import "fmt"

func main() {
    x := 0

    // Argument evaluated NOW (x=0)
    defer fmt.Println("deferred value:", x)

    // Closure captures x by reference
    defer func() {
        fmt.Println("deferred closure:", x)
    }()

    x = 42
    fmt.Println("main:", x)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
main: 42
deferred closure: 42
deferred value: 0
```

The `fmt.Println("deferred value:", x)` captured `x=0` at the time of the `defer` call. The closure captured the variable itself and sees `x=42`.

## Step 6 — Timing Functions with Defer

A clean pattern for timing function execution:

```go
package main

import (
    "fmt"
    "time"
)

func timer(name string) func() {
    start := time.Now()
    return func() {
        fmt.Printf("%s took %v\n", name, time.Since(start))
    }
}

func slowOperation() {
    defer timer("slowOperation")()

    time.Sleep(100 * time.Millisecond)
    fmt.Println("operation complete")
}

func main() {
    slowOperation()
}
```

Note the `()` at the end of `defer timer("slowOperation")()`. The call to `timer()` runs immediately (recording the start time), and the returned closure is what gets deferred.

### Intermediate Verification

```bash
go run main.go
```

Expected output (timing varies):

```
operation complete
slowOperation took 100.123ms
```

## Step 7 — Defer and Named Returns

`defer` can modify named return values:

```go
package main

import "fmt"

func doubleReturn() (result int) {
    defer func() {
        result *= 2
    }()

    return 5 // result is set to 5, then the defer doubles it
}

func main() {
    fmt.Println(doubleReturn())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
10
```

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Deferring inside a loop without scoping | All deferred calls wait until the function exits, not the loop iteration — can leak resources |
| Ignoring the error from `Close()` on writes | `f.Close()` on a writable file can fail if buffers have not been flushed |
| Deferring before checking the error | `defer f.Close()` before `if err != nil` attempts to close a nil file |
| Assuming defer runs at block scope | `defer` is function-scoped, not block-scoped |

To handle defer-in-loops, extract the body into a separate function:

```go
func processFile(path string) error {
    f, err := os.Open(path)
    if err != nil {
        return err
    }
    defer f.Close()
    // process f
    return nil
}

// In the loop:
for _, path := range paths {
    if err := processFile(path); err != nil {
        log.Println(err)
    }
}
```

## Verify What You Learned

1. Write a function that opens two files and uses `defer` to close both. Verify the close order is LIFO.
2. Write a `withTimer` wrapper function that prints how long any `func() error` takes to execute.
3. Write a function with a named return that uses `defer` to add context to errors (wrapping the error with additional information).

## What's Next

Next you will explore the **functional options pattern** — a powerful technique for configuring structs with optional parameters.

## Summary

- `defer` schedules a function call to run when the enclosing function returns
- Deferred calls execute in LIFO order (last deferred runs first)
- Use `defer` for file closing, mutex unlocking, and timing
- Arguments to deferred calls are evaluated immediately; closures capture by reference
- `defer` can modify named return values
- Be careful with `defer` inside loops — extract loop bodies into functions

## Reference

- [Go spec: Defer statements](https://go.dev/ref/spec#Defer_statements)
- [Effective Go: Defer](https://go.dev/doc/effective_go#defer)
- [Go blog: Defer, Panic, and Recover](https://go.dev/blog/defer-panic-and-recover)
