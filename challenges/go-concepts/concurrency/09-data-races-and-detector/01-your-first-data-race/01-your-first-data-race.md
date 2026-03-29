# 1. Your First Data Race

<!--
difficulty: basic
concepts: [data race, shared variable, concurrent write, non-determinism, lost update]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [goroutines, sync.WaitGroup]
-->

## Prerequisites
- Go 1.22+ installed
- Familiarity with launching goroutines using the `go` keyword
- Basic understanding of `sync.WaitGroup`

## Learning Objectives
After completing this exercise, you will be able to:
- **Define** what a data race is in terms of concurrent unsynchronized memory access
- **Reproduce** a data race using multiple goroutines writing to a shared variable
- **Observe** non-deterministic behavior caused by a data race
- **Explain** why the final result differs between runs

## Why Data Races Matter

A data race occurs when three conditions are ALL true simultaneously:

1. Two or more goroutines access the same memory location
2. At least one of the accesses is a write
3. There is no synchronization between the accesses

Data races are one of the most insidious bugs in concurrent programming because they produce **non-deterministic** results: the program may appear correct most of the time, then fail unpredictably under load or on different hardware.

The Go memory model explicitly states that a data race results in **undefined behavior**. This means the compiler and runtime make no guarantees about the outcome -- you cannot reason about the program's correctness when races exist.

In this exercise and the next four, we use the SAME counter problem (1000 goroutines x 1000 increments = expected 1,000,000) to demonstrate the progression from detecting races to fixing them.

## The Counter Problem

We launch 1000 goroutines, each incrementing a shared counter 1000 times. The expected result is 1,000,000. But without synchronization, the actual result is far less -- and different every time.

## Step 1 -- Understand the Code

Open `main.go`. The core function is `racyCounter`:

```go
package main

import (
    "fmt"
    "sync"
)

const (
    numGoroutines   = 1000
    incrementsPerGR = 1000
    expectedTotal   = numGoroutines * incrementsPerGR
)

func racyCounter() int {
    counter := 0
    var wg sync.WaitGroup

    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < incrementsPerGR; j++ {
                counter++ // DATA RACE: read-modify-write without synchronization
            }
        }()
    }

    wg.Wait()
    return counter
}
```

The operation `counter++` is **not atomic**. It consists of three CPU-level steps:
1. **READ** the current value of counter from memory
2. **ADD** one to the value
3. **WRITE** the new value back to memory

When multiple goroutines do this simultaneously, some increments are lost because goroutines read the same value before any of them writes back.

## Step 2 -- Run and Observe

### Verification
```bash
go run main.go
```

Sample output (your numbers WILL differ):
```
=== Your First Data Race ===
Expected result: 1000000 (1000 goroutines x 1000 increments)

--- Run 1 ---
Result: 547832     WRONG (lost 452168 increments)
--- Run 2 ---
Result: 611204     WRONG (lost 388796 increments)
--- Run 3 ---
Result: 503019     WRONG (lost 496981 increments)
--- Run 4 ---
Result: 589412     WRONG (lost 410588 increments)
--- Run 5 ---
Result: 528741     WRONG (lost 471259 increments)

Results across 5 runs: [547832 611204 503019 589412 528741]
All different! This non-determinism is the hallmark of a data race.
```

Run it several times. Each execution produces different results. This non-determinism is the unmistakable signature of a data race.

## Step 3 -- Understand Why the Race Happens

Think through this scenario with two goroutines accessing counter simultaneously:

```
Time    Goroutine A          Goroutine B          counter (memory)
----    -----------          -----------          ----------------
 1      READ counter (= 42)                       42
 2                           READ counter (= 42)  42
 3      WRITE counter (= 43)                      43
 4                           WRITE counter (= 43) 43  <-- increment LOST!
```

Both goroutines read 42, both compute 43, both write 43. Two increments happened, but the counter only went up by one. This is called a **lost update** and is the direct consequence of a data race.

With 1000 goroutines competing, thousands of increments are lost per second.

## Step 4 -- How Bad Can It Get?

The range of possible results:
- **Minimum**: 1000 (if every goroutine's entire 1000-increment loop overlaps with others)
- **Maximum**: 1,000,000 (if all goroutines happen to run sequentially -- extremely unlikely with 1000 goroutines)
- **Typical**: 400,000 - 700,000 depending on hardware and system load

The exact number depends on CPU architecture, number of cores, OS scheduler behavior, and current system load. This unpredictability is why data races are **undefined behavior** -- you cannot predict or control the outcome.

## Common Mistakes

### Thinking "It Worked Once, So It's Fine"
A data race may produce the correct result on some runs, especially on single-core machines or with few goroutines. The absence of symptoms does NOT prove the absence of the bug. Data races are undefined behavior -- they must be eliminated, not tolerated.

### Assuming Small Operations Are Atomic
Even `counter++` (or `counter += 1`) is NOT atomic in Go. It compiles to multiple machine instructions. Only operations from the `sync/atomic` package are guaranteed to be atomic (see exercise 05).

### Using time.Sleep as Synchronization
Sleeping does not synchronize memory. Even if you sleep "long enough," the compiler and CPU may reorder memory operations. Only proper synchronization primitives (`sync.Mutex`, channels, `sync/atomic`) establish happens-before relationships.

## Verify What You Learned

Answer these questions:
1. What three conditions must be true for a data race to exist?
2. Why does `counter++` produce wrong results when called from multiple goroutines?
3. If you run the program and get 1,000,000, does that prove there is no race? Why or why not?
4. Why is a data race worse than a regular bug?

## What's Next
Continue to [02-race-detector-flag](../02-race-detector-flag/02-race-detector-flag.md) to learn how Go's built-in race detector can automatically find this bug.

## Summary
- A data race occurs when two or more goroutines access the same variable concurrently, at least one access is a write, and there is no synchronization
- `counter++` is not atomic: it is read-modify-write, and concurrent execution causes **lost updates**
- Data race results are non-deterministic: the program produces different results on different runs
- Correct output on one run does NOT prove the absence of a data race
- Data races are **undefined behavior** in Go and must be eliminated
- Exercises 01-05 use the same counter problem (1000 goroutines x 1000 increments = 1,000,000 expected) to show progression from detection to fix

## Reference
- [Go Memory Model](https://go.dev/ref/mem)
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Go Spec: Statements](https://go.dev/ref/spec#Statements)
