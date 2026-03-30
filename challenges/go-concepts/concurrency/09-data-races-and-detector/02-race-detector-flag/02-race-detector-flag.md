---
difficulty: basic
concepts: [race detector, -race flag, happens-before, ThreadSanitizer, race report]
tools: [go]
estimated_time: 20m
bloom_level: understand
---

# 2. Race Detector Flag


## Learning Objectives
After completing this exercise, you will be able to:
- **Use** the `-race` flag with `go run`, `go test`, and `go build`
- **Read** and interpret the race detector output: goroutines, source lines, memory addresses
- **Add** `-race` to your CI pipeline for automated race detection
- **Explain** the performance overhead and when to enable detection

## Why the Race Detector

In exercise 01, you saw that the hit counter produced wrong numbers. But how do you find a race in a codebase with thousands of lines? You cannot rely on observing wrong output because data races are non-deterministic: the program might appear correct in testing and crash in production.

Go ships with a built-in race detector based on Google's **ThreadSanitizer** technology. When enabled, it instruments every memory access and tracks happens-before relationships between goroutine operations. If two accesses to the same memory lack a happens-before edge and at least one is a write, the detector reports a race.

Key properties:
- **Zero false positives**: every reported race is a real bug
- **Dynamic analysis**: it only detects races that occur during that specific execution
- **Performance cost**: roughly 5-10x CPU and 5-10x memory overhead
- **Use case**: development and CI, not production

## Step 1 -- Detect the Race in the Hit Counter

Create `main.go` with the hit counter from exercise 01, then run it with the race detector:

```go
package main

import (
	"fmt"
	"sync"
)

func racyHitCounter() int {
	hitCount := 0
	var wg sync.WaitGroup

	for handler := 0; handler < 100; handler++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for req := 0; req < 100; req++ {
				hitCount++
			}
		}(handler)
	}

	wg.Wait()
	return hitCount
}

func main() {
	fmt.Println("=== Race Detector Demo ===")
	result := racyHitCounter()
	fmt.Printf("Hit count: %d (expected 10000)\n", result)
}
```

### Verification

First, run without the race detector to see the wrong result:
```bash
go run main.go
```

Now run with the race detector enabled:
```bash
go run -race main.go
```

The race detector prints a report like this:
```
==================
WARNING: DATA RACE
Read at 0x00c00001e0b8 by goroutine 7:
  main.racyHitCounter.func1()
      /path/to/main.go:16 +0x38

Previous write at 0x00c00001e0b8 by goroutine 8:
  main.racyHitCounter.func1()
      /path/to/main.go:16 +0x4e

Goroutine 7 (running) created at:
  main.racyHitCounter()
      /path/to/main.go:12 +0x7c

Goroutine 8 (running) created at:
  main.racyHitCounter()
      /path/to/main.go:12 +0x7c
==================
```

The program still runs to completion despite the warning. The detector reports the race but does not stop execution.

## Step 2 -- Read the Race Report

The race detector report has four sections. Knowing how to read them is essential for debugging production race conditions:

**Section 1: "Read at 0x... by goroutine N"**
- The memory address (`0x00c00001e0b8`) is the `hitCount` variable
- `goroutine 7` is one of the handler goroutines
- `main.go:16` points to the exact line: `hitCount++`
- The `+0x38` is the offset within the function (useful for assembly debugging)

**Section 2: "Previous write at 0x... by goroutine M"**
- The SAME memory address, but a DIFFERENT goroutine
- "Previous" means the detector saw this access first, not that it happened first chronologically
- Same line `main.go:16` confirms both goroutines hit the same `hitCount++`

**Section 3 and 4: "Goroutine N/M created at:"**
- Stack traces showing WHERE each goroutine was launched
- `main.go:12` points to the `go func(id int)` line
- These traces let you find the `go` statement responsible

**Reading strategy**: Start at Section 1 (what variable is being raced), then check Section 2 (who else touches it), then Sections 3-4 (where those goroutines came from).

## Step 3 -- Add Race Detection to Your CI Pipeline

The `-race` flag works with multiple Go commands. The most important use is in CI:

```go
package main

import "fmt"

func main() {
	fmt.Println("=== Race Detection in CI ===")
	fmt.Println()
	fmt.Println("Add to your CI pipeline (GitHub Actions, GitLab CI, etc.):")
	fmt.Println()
	fmt.Println("  # Run all tests with race detection")
	fmt.Println("  go test -race ./...")
	fmt.Println()
	fmt.Println("  # Build an instrumented binary for integration tests")
	fmt.Println("  go build -race -o myservice ./cmd/server")
	fmt.Println("  ./myservice &")
	fmt.Println("  # run integration tests against it")
	fmt.Println("  # any race during the test run will be reported")
	fmt.Println()
	fmt.Println("  # Log race reports to a file")
	fmt.Println("  GORACE=\"log_path=race.log\" go test -race ./...")
	fmt.Println()
	fmt.Println("Performance overhead:")
	fmt.Println("  - CPU:    ~10x slower")
	fmt.Println("  - Memory: ~5-10x more")
	fmt.Println("  - Binary: significantly larger")
	fmt.Println()
	fmt.Println("NEVER deploy a -race binary to production.")
	fmt.Println("Use it in dev, test, and CI only.")
}
```

A typical CI configuration:
```yaml
# .github/workflows/test.yml (relevant step)
- name: Test with race detector
  run: go test -race -count=1 ./...
```

The `-count=1` flag disables test caching, ensuring every test actually runs with the race detector on every pipeline execution. Without it, cached test results skip the race detection.

## Step 4 -- Understand What the Detector Can and Cannot Find

The race detector only finds races that **execute during the test run**. Create this program to see both a detected race and proper synchronization:

```go
package main

import (
	"fmt"
	"sync"
)

func racyCounter() int {
	hitCount := 0
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hitCount++ // RACE: no synchronization
		}()
	}

	wg.Wait()
	return hitCount
}

func synchronizedCounter() int {
	hitCount := 0
	done := make(chan struct{})

	go func() {
		hitCount = 42 // write
		close(done)   // synchronization point
	}()

	<-done // happens-after close(done)
	return hitCount
}

func main() {
	fmt.Println("=== What the Detector Finds ===")
	fmt.Println()

	fmt.Printf("Racy counter: %d (WILL trigger race warning)\n", racyCounter())
	fmt.Printf("Synchronized: %d (NO race warning)\n", synchronizedCounter())

	fmt.Println()
	fmt.Println("The detector uses happens-before analysis, not timing.")
	fmt.Println("Channel close -> receive establishes a happens-before edge.")
	fmt.Println()
	fmt.Println("What the detector CANNOT find:")
	fmt.Println("  - Deadlocks")
	fmt.Println("  - Livelocks")
	fmt.Println("  - Goroutine leaks")
	fmt.Println("  - Logical race conditions (timing-dependent behavior that is technically race-free)")
	fmt.Println("  - Races in code paths that were not exercised during the run")
}
```

### Verification
```bash
go run -race main.go
```

The `racyCounter` function triggers `WARNING: DATA RACE`. The `synchronizedCounter` function produces no warning because the channel close establishes a happens-before relationship.

## Common Mistakes

### Ignoring Race Detector Warnings
The race detector has **zero false positives**. Every reported race is a real bug. Never dismiss a race report as a "false alarm." Fix every race it reports.

### Assuming No Report Means No Races
The race detector only finds races that **occur during execution**. If a code path is not exercised, its races will not be found. Write comprehensive tests and run them with `-race` to maximize coverage.

### Running -race in Production
The race detector adds significant overhead (5-10x slower, 5-10x more memory). Use it in development and CI, not in production binaries. A `-race` binary under production load will be unacceptably slow and use too much memory.

### Thinking the Detector Finds All Concurrency Bugs
The race detector finds **data races** specifically. It does NOT find deadlocks, livelocks, starvation, goroutine leaks, or logical race conditions where the program is technically race-free but still has timing-dependent behavior.

## Verify What You Learned

1. What does "happens-before" mean, and how does it relate to race detection?
2. Name three synchronization operations that establish happens-before relationships in Go.
3. Can the race detector produce false positives? Can it miss real races?
4. Why should you add `-count=1` when running `go test -race` in CI?

## What's Next
Continue to [03-fix-race-with-mutex](../03-fix-race-with-mutex/03-fix-race-with-mutex.md) to fix the hit counter race using `sync.Mutex`.

## Summary
- The `-race` flag enables Go's built-in race detector based on ThreadSanitizer
- Use `go run -race`, `go test -race`, or `go build -race` to enable it
- The race report shows: the conflicting accesses, their goroutines, source locations, and goroutine creation stacks
- Add `go test -race -count=1 ./...` to your CI pipeline to catch races automatically
- Zero false positives: every reported race is real
- The detector uses happens-before analysis, not just concurrent access detection
- Can only detect races that occur during execution, not all possible races
- Use it in development and CI, not production (5-10x CPU and memory overhead)

## Reference
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Go Memory Model](https://go.dev/ref/mem)
- [ThreadSanitizer Algorithm](https://github.com/google/sanitizers/wiki/ThreadSanitizerAlgorithm)
- [Go Command: Testing Flags](https://pkg.go.dev/cmd/go#hdr-Testing_flags)
