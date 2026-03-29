# 2. Race Detector Flag

<!--
difficulty: basic
concepts: [race detector, -race flag, happens-before, ThreadSanitizer, race report]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [goroutines, sync.WaitGroup, data race concept]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercise 01 (Your First Data Race)
- Understanding of what a data race is

## Learning Objectives
After completing this exercise, you will be able to:
- **Use** the `-race` flag with `go run`, `go test`, and `go build`
- **Read** and interpret the race detector output
- **Identify** the goroutines, source lines, and memory addresses involved in a race
- **Explain** that the race detector uses happens-before analysis, not just concurrent access detection

## Why the Race Detector
Finding data races by inspecting code or observing wrong output is unreliable. A race might only manifest under specific scheduling conditions, high load, or on certain hardware. Go ships with a built-in race detector based on Google's ThreadSanitizer technology. When enabled, it instruments every memory access and tracks happens-before relationships between goroutine operations. If two accesses to the same memory lack a happens-before edge and at least one is a write, the detector reports a race.

The race detector does not use heuristics or sampling. It is precise: every reported race is a real race. However, it only detects races that actually occur during a given execution, so it requires good test coverage to find all races.

The performance overhead is roughly 5-10x in CPU and 5-10x in memory, so it is typically used during development and CI, not in production.

## Step 1 -- Run the Racy Program with -race

The starter `main.go` contains the same racy counter from exercise 01 (already implemented -- this exercise focuses on the detector). Run it normally first:

```bash
go run main.go
```

You will see the wrong counter value. Now run it with the race detector enabled:

```bash
go run -race main.go
```

The race detector will print a report that looks like this:

```
==================
WARNING: DATA RACE
Read at 0x00c00001e0b8 by goroutine 7:
  main.racyCounter.func1()
      /path/to/main.go:24 +0x38

Previous write at 0x00c00001e0b8 by goroutine 8:
  main.racyCounter.func1()
      /path/to/main.go:24 +0x4e

Goroutine 7 (running) created at:
  main.racyCounter()
      /path/to/main.go:20 +0x7c

Goroutine 8 (running) created at:
  main.racyCounter()
      /path/to/main.go:20 +0x7c
==================
```

### Intermediate Verification
Confirm that `go run -race main.go` produces a `WARNING: DATA RACE` message. The program may still run to completion despite the warning.

## Step 2 -- Read the Race Report

The race detector report has four sections. Implement `explainRaceReport` by adding print statements that describe each section:

```go
func explainRaceReport() {
    fmt.Println("=== How to Read a Race Detector Report ===")
    fmt.Println()
    fmt.Println("Section 1: 'Read at 0x... by goroutine N'")
    fmt.Println("  -> The memory address and goroutine performing one of the conflicting accesses.")
    fmt.Println("  -> Includes the source file and line number.")
    fmt.Println()
    fmt.Println("Section 2: 'Previous write at 0x... by goroutine M'")
    fmt.Println("  -> The other conflicting access (previous in detection order, not necessarily in time).")
    fmt.Println("  -> Same memory address, different goroutine.")
    fmt.Println()
    fmt.Println("Section 3: 'Goroutine N created at:'")
    fmt.Println("  -> Stack trace showing WHERE goroutine N was launched.")
    fmt.Println("  -> Helps you trace back to the go statement that started it.")
    fmt.Println()
    fmt.Println("Section 4: 'Goroutine M created at:'")
    fmt.Println("  -> Same as above for the other goroutine.")
    fmt.Println()
    fmt.Println("Key insight: the detector found two accesses to the SAME memory address")
    fmt.Println("by DIFFERENT goroutines with no happens-before relationship between them.")
}
```

### Intermediate Verification
```bash
go run -race main.go
```
The explanation should print before the race is triggered, helping you map the explanation to the actual report.

## Step 3 -- Understand Happens-Before

The race detector does not simply check "were two goroutines running at the same time?" Instead, it tracks happens-before relationships. An access A happens-before access B if there is a synchronization operation (channel send/receive, mutex lock/unlock, WaitGroup operations) that orders them.

Edit the `happensBefore` function to demonstrate a synchronized access that does NOT trigger the detector:

```go
func happensBefore() {
    fmt.Println("=== Happens-Before: No Race ===")
    counter := 0
    done := make(chan struct{})

    go func() {
        counter = 42 // write
        close(done)  // synchronization point
    }()

    <-done          // synchronization point: happens-after close(done)
    fmt.Printf("counter = %d (no race: channel establishes happens-before)\n", counter)
}
```

The channel operation `close(done)` happens-before `<-done`, which means the write to `counter` in the goroutine happens-before the read in `main`. The detector sees this and does not report a race.

### Intermediate Verification
```bash
go run -race main.go
```
The `happensBefore` function should produce NO race warning. The `racyCounter` function will still produce warnings.

## Step 4 -- Other Ways to Use -race

The `-race` flag works with multiple Go commands:

```bash
# Run a program with race detection
go run -race main.go

# Run tests with race detection
go test -race ./...

# Build a binary with race detection
go build -race -o myprogram main.go
./myprogram
```

Note: the binary built with `-race` will be larger and slower, but it will detect races at runtime whenever they occur.

## Common Mistakes

### Ignoring Race Detector Warnings
The race detector has zero false positives. Every reported race is a real bug. Never dismiss a race report as a "false alarm." Fix every race it reports.

### Assuming No Report Means No Races
The race detector only finds races that occur during execution. If a code path is not exercised, its races will not be found. Use comprehensive tests with `-race` to maximize coverage.

### Running -race in Production
The race detector adds significant overhead (5-10x slower, 5-10x more memory). Use it in development and CI, not in production binaries.

### Thinking the Detector Finds All Concurrency Bugs
The race detector finds data races specifically. It does NOT find deadlocks, livelocks, starvation, or logical race conditions (where the program is technically race-free but still has timing-dependent behavior).

## Verify What You Learned

Answer these questions:
1. What is the difference between "concurrent access" and a "data race"?
2. What does "happens-before" mean, and how does it relate to race detection?
3. Name three synchronization operations that establish happens-before relationships in Go.
4. Can the race detector produce false positives? Can it miss real races?

## What's Next
Continue to [03-fix-race-with-mutex](../03-fix-race-with-mutex/03-fix-race-with-mutex.md) to learn how to fix this race using `sync.Mutex`.

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
