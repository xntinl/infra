# 1. Your First Data Race

<!--
difficulty: basic
concepts: [data race, shared variable, concurrent write, non-determinism]
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
A data race occurs when two or more goroutines access the same memory location concurrently, and at least one of the accesses is a write, with no synchronization between them. Data races are one of the most insidious bugs in concurrent programming because they produce non-deterministic results: the program may appear to work correctly most of the time, then fail unpredictably under load or on different hardware.

The Go memory model explicitly states that a data race results in undefined behavior. This means the compiler and runtime make no guarantees about the outcome -- you cannot reason about the program's correctness when races exist.

In this exercise, you will create a program with an intentional data race to see the problem firsthand. The next seven exercises in this section will teach you how to detect and fix races.

## Step 1 -- Create a Shared Counter

Edit `main.go` and implement the `racyCounter` function. Launch 1000 goroutines, each incrementing a shared `counter` variable 1000 times:

```go
func racyCounter() int {
    counter := 0
    var wg sync.WaitGroup

    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 1000; j++ {
                counter++ // unsynchronized write to shared variable
            }
        }()
    }

    wg.Wait()
    return counter
}
```

The operation `counter++` is not atomic. It consists of three steps: read the current value, add one, write the result back. When multiple goroutines do this simultaneously, some increments are lost because goroutines read the same value before any of them writes back.

### Intermediate Verification
```bash
go run main.go
```
Expected: the counter should be 1,000,000 (1000 goroutines x 1000 increments), but you will see a number less than 1,000,000.

## Step 2 -- Observe Non-Determinism

Implement the `main` function to run `racyCounter` multiple times and observe that the result changes between runs:

```go
func main() {
    fmt.Println("=== Data Race Demonstration ===")
    fmt.Println("Expected result: 1000000")
    fmt.Println()

    for run := 1; run <= 5; run++ {
        result := racyCounter()
        status := "CORRECT"
        if result != 1000000 {
            status = "WRONG (race!)"
        }
        fmt.Printf("Run %d: counter = %d  %s\n", run, result, status)
    }

    fmt.Println()
    fmt.Println("Notice: results vary between runs.")
    fmt.Println("This non-determinism is the hallmark of a data race.")
    fmt.Println()
    fmt.Println("Next exercise: use 'go run -race main.go' to detect this race automatically.")
}
```

### Intermediate Verification
```bash
go run main.go
```
Sample output (your numbers will differ):
```
=== Data Race Demonstration ===
Expected result: 1000000

Run 1: counter = 548923  WRONG (race!)
Run 2: counter = 611047  WRONG (race!)
Run 3: counter = 503891  WRONG (race!)
Run 4: counter = 587102  WRONG (race!)
Run 5: counter = 529467  WRONG (race!)

Notice: results vary between runs.
This non-determinism is the hallmark of a data race.
```

Run it several times and compare the results. They will be different each time.

## Step 3 -- Understand Why the Race Happens

Think through the following scenario with two goroutines:

```
Time    Goroutine A          Goroutine B          counter (memory)
----    -----------          -----------          ----------------
 1      READ counter (= 42)                       42
 2                           READ counter (= 42)  42
 3      WRITE counter (= 43)                      43
 4                           WRITE counter (= 43) 43  <-- increment lost!
```

Both goroutines read 42, both compute 43, both write 43. Two increments happened, but the counter only went up by one. This is called a **lost update**, and it is the direct consequence of a data race.

## Common Mistakes

### Thinking "It Worked Once, So It's Fine"
A data race may produce the correct result on some runs, especially on single-core machines or with few goroutines. The absence of symptoms does not prove the absence of the bug. Data races are undefined behavior -- they must be eliminated, not tolerated.

### Assuming Small Operations Are Atomic
Even `counter++` (or `counter += 1`) is not atomic in Go. It compiles to multiple machine instructions. Only operations from the `sync/atomic` package are guaranteed to be atomic.

### Using time.Sleep as Synchronization
Sleeping does not synchronize memory. Even if you sleep "long enough," the compiler and CPU may reorder memory operations. Only proper synchronization primitives (`sync.Mutex`, channels, `sync/atomic`) establish happens-before relationships.

## Verify What You Learned

Answer these questions:
1. What three conditions must be true for a data race to exist?
2. Why does `counter++` produce wrong results when called from multiple goroutines?
3. If you run the program and get 1,000,000, does that prove there is no race? Why or why not?

## What's Next
Continue to [02-race-detector-flag](../02-race-detector-flag/02-race-detector-flag.md) to learn how Go's built-in race detector can automatically find this bug.

## Summary
- A data race occurs when two or more goroutines access the same variable concurrently and at least one access is a write, without synchronization
- The `counter++` operation is not atomic: it is read-modify-write, and concurrent execution causes lost updates
- Data race results are non-deterministic -- the program produces different results on different runs
- Correct output on one run does not prove the absence of a data race
- Data races are undefined behavior in Go and must be eliminated

## Reference
- [Go Memory Model](https://go.dev/ref/mem)
- [Go Blog: Introducing the Go Race Detector](https://go.dev/blog/race-detector)
- [Go Spec: Statements](https://go.dev/ref/spec#Statements)
