# 9. Pointer Aliasing and Data Races

<!--
difficulty: advanced
concepts: [pointer-aliasing, data-races, race-detector, sync-mutex, atomic-operations, shared-state]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [pointer-basics, pointers-to-structs, pointers-in-slices-and-maps, goroutines-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of pointers (exercises 01-08)
- Basic familiarity with goroutines (`go` keyword)

## The Problem

Pointer aliasing occurs when two or more variables hold pointers to the same memory. This is harmless in single-threaded code, but when multiple goroutines hold aliased pointers and at least one writes, you have a data race. Data races cause unpredictable behavior: corrupted data, panics, or silent wrong results.

Go provides the `-race` flag to detect races at runtime and `sync.Mutex` / `sync/atomic` to prevent them. Your task: create, detect, and fix data races caused by pointer aliasing.

## Hints

<details>
<summary>Hint 1: Creating a data race with aliased pointers</summary>

```go
type Counter struct{ N int }

func main() {
    c := &Counter{}
    for i := 0; i < 1000; i++ {
        go func() { c.N++ }() // all goroutines alias the same pointer
    }
    time.Sleep(time.Second)
    fmt.Println(c.N) // result is unpredictable
}
```

Run with `go run -race main.go` to see the race detector fire.
</details>

<details>
<summary>Hint 2: Detecting races</summary>

```bash
go run -race main.go
go test -race ./...
```

The race detector instruments memory accesses and reports when two goroutines access the same address concurrently with at least one write. It prints the goroutine stacks involved.
</details>

<details>
<summary>Hint 3: Fixing with sync.Mutex</summary>

```go
type SafeCounter struct {
    mu sync.Mutex
    n  int
}

func (c *SafeCounter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.n++
}
```

The mutex ensures only one goroutine accesses `n` at a time. All goroutines still alias the same `*SafeCounter`, but the mutex serializes access.
</details>

<details>
<summary>Hint 4: Fixing with sync/atomic</summary>

For simple counters, atomic operations avoid the overhead of a mutex:

```go
var counter int64

func increment() {
    atomic.AddInt64(&counter, 1)
}
```

Atomic operations are lock-free but limited to simple reads, writes, and add/compare-and-swap.
</details>

<details>
<summary>Hint 5: Eliminating aliasing by copying</summary>

Sometimes the best fix is to not share a pointer at all:

```go
type Config struct { MaxRetries int }

func worker(cfg Config) { // value copy, not pointer
    // each goroutine has its own copy -- no aliasing
}
```

If goroutines only read the data and never write it, a shared pointer is safe. But if any goroutine writes, either synchronize or copy.
</details>

## Requirements

1. Write a program that creates a data race through pointer aliasing:
   - A shared struct accessed via the same pointer from multiple goroutines
   - At least one goroutine writes to the struct
2. Run the program with `go run -race main.go` and capture the race detector output
3. Fix the race using three different approaches, each in its own function:
   - `sync.Mutex` to protect the shared pointer
   - `sync/atomic` for a counter field
   - Eliminating aliasing by passing value copies to goroutines
4. Write a test with `-race` that verifies all three approaches are race-free
5. Demonstrate a subtle aliasing bug: a slice of pointers where goroutines process different indices but the race detector still fires (explain why or confirm it does not)

## Verification

1. `go run -race main.go` should report a data race for the unfixed version
2. `go run -race main.go` should report zero races for all three fixed versions
3. `go test -race ./...` should pass with no race warnings
4. The program should produce a deterministic final count (e.g., exactly 10000 for 10000 increments)

Check your understanding:
- Why does the race detector not catch every possible race in one run?
- What is the performance cost of running with `-race`?
- When is a read-only shared pointer safe without synchronization?
- Why is `counter++` not atomic even though it looks like one operation?

## What's Next

Continue to [10 - Designing Pointer-Safe APIs](../10-designing-pointer-safe-apis/10-designing-pointer-safe-apis.md) to learn how to design APIs that make pointer misuse difficult.

## Reference

- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Go Memory Model](https://go.dev/ref/mem)
- [sync.Mutex](https://pkg.go.dev/sync#Mutex)
- [sync/atomic](https://pkg.go.dev/sync/atomic)
- [Go Data Race Detector](https://go.dev/doc/articles/race_detector)
