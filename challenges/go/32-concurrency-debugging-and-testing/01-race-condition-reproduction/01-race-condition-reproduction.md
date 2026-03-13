# 1. Race Condition Reproduction

<!--
difficulty: advanced
concepts: [data-race, race-detector, tsan, race-flag, happens-before, memory-model]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, sync-mutex, channels-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, mutexes, and channels
- Familiarity with the Go memory model (happens-before relationships)

## Learning Objectives

After completing this exercise, you will be able to:

- **Reproduce** data races in Go programs intentionally to understand their mechanics
- **Analyze** race detector output to identify the conflicting accesses
- **Classify** different types of races: read-write, write-write, and compound races
- **Apply** the race detector effectively during development and CI

## Why Race Condition Reproduction Matters

Data races are among the hardest bugs to find because they are non-deterministic. A program with a race might pass thousands of tests and then fail in production under different timing conditions. Go's built-in race detector (`-race` flag) uses ThreadSanitizer (TSan) to detect races at runtime. Learning to reproduce and read race reports is an essential debugging skill.

You cannot fix what you cannot reproduce. This exercise teaches you to create minimal race examples, interpret the detector's output, and understand which fixes are correct.

## The Problem

You will create several programs that contain intentional data races, run them with the race detector, interpret the output, and fix each race using different synchronization mechanisms.

## Requirements

1. **Read-write race** -- create a goroutine that reads a variable while another writes it; capture and explain the race detector output
2. **Write-write race** -- create two goroutines writing to the same variable; show that the result is unpredictable
3. **Map race** -- demonstrate the runtime fatal error from concurrent map writes (distinct from race detector)
4. **Struct field race** -- show that races on different fields of the same struct are independent, but races on the same field are detected
5. **Slice race** -- demonstrate a race on slice append (shared backing array)
6. **Fix each race** -- fix using the appropriate mechanism: mutex, channel, atomic, or copy-on-write
7. **CI integration** -- write a test with `go test -race` that fails when the race exists and passes when fixed

## Hints

<details>
<summary>Hint 1: Reading race detector output</summary>

The race detector prints two stacks: the current access and the previous conflicting access. Look for:

```
WARNING: DATA RACE
Read at 0x00c0000b4010 by goroutine 7:
  main.main.func1()
      /path/main.go:15 +0x30

Previous write at 0x00c0000b4010 by goroutine 8:
  main.main.func2()
      /path/main.go:21 +0x40

Goroutine 7 (running) created at:
  main.main()
      /path/main.go:14 +0x90
```

The file and line numbers tell you exactly where the conflicting accesses are.

</details>

<details>
<summary>Hint 2: Reproducing map race</summary>

```go
m := make(map[int]int)
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        m[n] = n // concurrent map write -- fatal error, not just a race
    }(i)
}
wg.Wait()
```

Note: concurrent map writes cause a runtime panic (`fatal error: concurrent map writes`), which is different from a data race. The runtime detects this even without `-race`.

</details>

<details>
<summary>Hint 3: Atomic fix for simple counters</summary>

```go
import "sync/atomic"

var counter atomic.Int64

// In goroutine:
counter.Add(1)

// Reading:
value := counter.Load()
```

</details>

<details>
<summary>Hint 4: Race on slice append</summary>

```go
var results []int
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        results = append(results, n) // RACE: append modifies the slice header
    }(i)
}
```

Fix with a mutex or use a channel to collect results.

</details>

## Verification

```bash
# Each racy program should fail
go run -race racy_readwrite.go 2>&1 | grep "DATA RACE"
go run -race racy_writewrite.go 2>&1 | grep "DATA RACE"
go run -race racy_slice.go 2>&1 | grep "DATA RACE"

# Fixed versions should pass
go run -race fixed_readwrite.go  # no warnings
go run -race fixed_writewrite.go  # no warnings
go run -race fixed_slice.go       # no warnings

# Test suite
go test -v -race ./...
```

Confirm:
- Race detector reports the exact lines of conflicting accesses
- Each race type produces a different but interpretable report
- All fixed versions pass with `-race` enabled
- Tests demonstrate both the racy and fixed behavior

## What's Next

Continue to [02 - Goroutine Leak Detection with goleak](../02-goroutine-leak-detection-goleak/02-goroutine-leak-detection-goleak.md) to detect goroutines that are never cleaned up.

## Summary

- Go's race detector (`-race` flag) uses ThreadSanitizer to detect data races at runtime
- The detector reports the two conflicting accesses with full stack traces and goroutine creation sites
- Different race types: read-write, write-write, map concurrent write (fatal), slice append, struct fields
- Concurrent map writes cause a runtime panic even without `-race`
- Fix races with mutexes, channels, atomics, or by eliminating shared state
- Always run `go test -race` in CI to catch races early

## Reference

- [Data Race Detector](https://go.dev/doc/articles/race_detector)
- [The Go Memory Model](https://go.dev/ref/mem)
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
