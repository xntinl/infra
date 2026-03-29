---
difficulty: basic
concepts: [race detector, -race flag, happens-before, ThreadSanitizer, race report]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [goroutines, sync.WaitGroup, data race concept]
---

# 2. Race Detector Flag


## Learning Objectives
After completing this exercise, you will be able to:
- **Use** the `-race` flag with `go run`, `go test`, and `go build`
- **Read** and interpret the race detector output
- **Identify** the goroutines, source lines, and memory addresses involved in a race
- **Explain** that the race detector uses happens-before analysis, not just concurrent access detection

## Why the Race Detector

Finding data races by inspecting code or observing wrong output is unreliable. A race might only manifest under specific scheduling conditions, high load, or on certain hardware. Go ships with a built-in race detector based on Google's **ThreadSanitizer** technology.

When enabled, the race detector instruments every memory access and tracks happens-before relationships between goroutine operations. If two accesses to the same memory lack a happens-before edge and at least one is a write, the detector reports a race.

Key properties:
- **Zero false positives**: every reported race is a real bug
- **Dynamic analysis**: it only detects races that occur during that specific execution
- **Performance cost**: roughly 5-10x CPU and 5-10x memory overhead
- **Use case**: development and CI, not production

## Step 1 -- Run with -race

The `main.go` contains the same racy counter from exercise 01. Run it normally first:

```bash
go run main.go
```

You see the wrong counter value. Now run with the race detector:

```bash
go run -race main.go
```

### Verification
The race detector prints a report like this:
```
==================
WARNING: DATA RACE
Read at 0x00c00001e0b8 by goroutine 7:
  main.racyCounter.func1()
      /path/to/main.go:52 +0x38

Previous write at 0x00c00001e0b8 by goroutine 8:
  main.racyCounter.func1()
      /path/to/main.go:52 +0x4e

Goroutine 7 (running) created at:
  main.racyCounter()
      /path/to/main.go:48 +0x7c

Goroutine 8 (running) created at:
  main.racyCounter()
      /path/to/main.go:48 +0x7c
==================
```

The program may still run to completion despite the warning. The detector reports the race but does not stop execution.

## Step 2 -- Read the Race Report

The race detector report has four sections:

**Section 1: "Read at 0x... by goroutine N"**
- The memory address and goroutine performing one of the conflicting accesses
- Includes the source file and line number
- The `+0x38` is the offset within the function (useful for assembly debugging)

**Section 2: "Previous write at 0x... by goroutine M"**
- The other conflicting access (previous in detection order, not necessarily in time)
- Same memory address, different goroutine
- "Previous" means the detector saw this access first, not that it happened first chronologically

**Section 3: "Goroutine N created at:"**
- Stack trace showing WHERE goroutine N was launched
- Helps you trace back to the `go` statement that started it

**Section 4: "Goroutine M created at:"**
- Same as above for the other goroutine
- Together with Section 3, these tell you exactly which two `go` statements created the conflicting goroutines

**Key insight**: the detector found two accesses to the SAME memory address by DIFFERENT goroutines with no happens-before relationship between them.

## Step 3 -- Understand Happens-Before

The race detector does NOT simply check "were two goroutines running at the same time?" Instead, it tracks **happens-before relationships**. An access A happens-before access B if there is a synchronization operation that orders them.

Operations that establish happens-before in Go:
- Channel send happens-before the corresponding receive
- `close(ch)` happens-before `<-ch` returns the zero value
- `mu.Unlock()` happens-before the next `mu.Lock()`
- `wg.Done()` happens-before `wg.Wait()` returns
- `once.Do(f)` completes happens-before any other `once.Do` returns

The `main.go` includes `happensBefore()` which demonstrates synchronized access that does NOT trigger the detector:

```go
package main

import (
    "fmt"
    "sync"
)

func happensBefore() {
    // Example A: channel close establishes happens-before.
    counter := 0
    done := make(chan struct{})

    go func() {
        counter = 42 // write
        close(done)  // synchronization point
    }()

    <-done // happens-after close(done)
    fmt.Printf("counter = %d (no race)\n", counter)

    // Example B: WaitGroup.Done happens-before Wait.
    var wg sync.WaitGroup
    value := 0

    wg.Add(1)
    go func() {
        defer wg.Done()
        value = 100 // write
    }()

    wg.Wait()
    fmt.Printf("counter = %d (no race)\n", value)
}
```

### Verification
```bash
go run -race main.go
```
The `happensBefore` function produces NO race warning. The `racyCounter` function still produces warnings. This proves the detector analyzes synchronization relationships, not just temporal overlap.

## Step 4 -- Usage Reference

The `-race` flag works with multiple Go commands:

```bash
# Run a program with race detection
go run -race main.go

# Run tests with race detection (recommended in CI)
go test -race ./...

# Build an instrumented binary
go build -race -o myprogram main.go
./myprogram

# Log race reports to a file instead of stderr
GORACE="log_path=race.log" go run -race main.go
```

The binary built with `-race` will be larger and slower, but it detects races at runtime whenever they occur. This is useful for integration tests that exercise many code paths.

## Common Mistakes

### Ignoring Race Detector Warnings
The race detector has **zero false positives**. Every reported race is a real bug. Never dismiss a race report as a "false alarm." Fix every race it reports.

### Assuming No Report Means No Races
The race detector only finds races that **occur during execution**. If a code path is not exercised, its races will not be found. Use comprehensive tests with `-race` to maximize coverage.

### Running -race in Production
The race detector adds significant overhead (5-10x slower, 5-10x more memory). Use it in development and CI, not in production binaries.

### Thinking the Detector Finds All Concurrency Bugs
The race detector finds **data races** specifically. It does NOT find:
- Deadlocks
- Livelocks
- Starvation
- Logical race conditions (where the program is technically race-free but still has timing-dependent behavior)

## Verify What You Learned

1. What is the difference between "concurrent access" and a "data race"?
2. What does "happens-before" mean, and how does it relate to race detection?
3. Name three synchronization operations that establish happens-before relationships in Go.
4. Can the race detector produce false positives? Can it miss real races?

## What's Next
Continue to [03-fix-race-with-mutex](../03-fix-race-with-mutex/03-fix-race-with-mutex.md) to fix the counter race using `sync.Mutex`.

## Summary
- The `-race` flag enables Go's built-in race detector based on ThreadSanitizer
- Use `go run -race`, `go test -race`, or `go build -race` to enable it
- The race report shows: the conflicting accesses, their goroutines, source locations, and goroutine creation stacks
- The detector uses happens-before analysis, not just concurrent access detection
- Zero false positives: every reported race is real
- Can only detect races that occur during execution, not all possible races
- Use it in development and CI, not production (5-10x overhead)

## Reference
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Go Memory Model](https://go.dev/ref/mem)
- [ThreadSanitizer Algorithm](https://github.com/google/sanitizers/wiki/ThreadSanitizerAlgorithm)
- [Go Command: Testing Flags](https://pkg.go.dev/cmd/go#hdr-Testing_flags)
