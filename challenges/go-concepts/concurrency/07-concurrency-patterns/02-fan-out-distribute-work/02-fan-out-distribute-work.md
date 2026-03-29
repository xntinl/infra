# 2. Fan-Out: Distribute Work

<!--
difficulty: intermediate
concepts: [fan-out, work distribution, channel sharing, goroutine workers]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, pipeline pattern]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines and channels
- Familiarity with `sync.WaitGroup`
- Completion of exercise 01 (pipeline pattern)

## Learning Objectives
After completing this exercise, you will be able to:
- **Distribute** work from a single channel to multiple concurrent workers
- **Explain** how Go's channel semantics naturally provide work distribution
- **Observe** non-deterministic distribution across workers
- **Coordinate** worker completion with `sync.WaitGroup`

## Why Fan-Out
Fan-out is the pattern of distributing work from a single source to multiple goroutines. It is one of the most natural patterns in Go because of how channels work: when multiple goroutines receive from the same channel, the runtime guarantees that each value is delivered to exactly one receiver. There is no duplication, no need for external coordination -- the channel itself acts as a thread-safe work queue.

This pattern is critical for parallelizing CPU-bound or I/O-bound stages in a pipeline. If one stage is a bottleneck, you can fan it out to N workers, each pulling from the same input channel, processing independently, and feeding results downstream. The key mental model is a single funnel (the channel) feeding multiple workers.

Unlike thread pools in other languages that require explicit queue data structures, mutexes, and condition variables, Go's fan-out requires nothing beyond a shared channel and the `go` keyword.

## Step 1 -- Single-Channel Work Distribution

Create a work channel and launch multiple workers that all read from it. Observe how Go distributes the values.

Edit `main.go` and implement the `basicFanOut` function:

```go
func basicFanOut() {
    fmt.Println("=== Basic Fan-Out ===")
    jobs := make(chan int, 10)

    var wg sync.WaitGroup
    for w := 1; w <= 3; w++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for job := range jobs {
                fmt.Printf("  worker %d processing job %d\n", id, job)
                time.Sleep(50 * time.Millisecond) // simulate work
            }
        }(w)
    }

    for j := 1; j <= 9; j++ {
        jobs <- j
    }
    close(jobs)

    wg.Wait()
    fmt.Println()
}
```

Three workers compete for 9 jobs. Each job goes to exactly one worker. The distribution is non-deterministic.

### Intermediate Verification
```bash
go run main.go
```
Expected: all 9 jobs processed, roughly 3 per worker (order varies):
```
=== Basic Fan-Out ===
  worker 1 processing job 1
  worker 2 processing job 2
  worker 3 processing job 3
  ...
```

## Step 2 -- Fan-Out a Pipeline Stage

Integrate fan-out into a pipeline. Create a generator stage, then fan out the processing stage to multiple workers.

```go
func generate(start, end int) <-chan int {
    out := make(chan int)
    go func() {
        for i := start; i <= end; i++ {
            out <- i
        }
        close(out)
    }()
    return out
}

func fanOutSquare(in <-chan int, numWorkers int) <-chan int {
    results := make(chan int, numWorkers)
    var wg sync.WaitGroup

    for w := 0; w < numWorkers; w++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for n := range in {
                result := n * n
                fmt.Printf("  worker %d: %d^2 = %d\n", id, n, result)
                results <- result
                time.Sleep(30 * time.Millisecond)
            }
        }(w)
    }

    go func() {
        wg.Wait()
        close(results)
    }()

    return results
}
```

The `fanOutSquare` function launches N workers that all read from the same input channel. A separate goroutine waits for all workers to finish and then closes the results channel.

### Intermediate Verification
```bash
go run main.go
```
Expected: each number 1-12 squared, distributed across workers:
```
=== Fan-Out Pipeline ===
  worker 0: 1^2 = 1
  worker 1: 2^2 = 4
  ...
Results: 1 4 9 16 ...
```

## Step 3 -- Observe Distribution Under Load

Implement a function that shows how distribution changes with different worker counts and workload characteristics.

```go
func distributionAnalysis() {
    fmt.Println("=== Distribution Analysis ===")
    const totalJobs = 20

    for _, numWorkers := range []int{1, 3, 5} {
        fmt.Printf("\n  Workers: %d\n", numWorkers)
        counts := make(map[int]int)
        var mu sync.Mutex
        var wg sync.WaitGroup

        jobs := make(chan int, totalJobs)
        for w := 0; w < numWorkers; w++ {
            wg.Add(1)
            go func(id int) {
                defer wg.Done()
                for range jobs {
                    mu.Lock()
                    counts[id]++
                    mu.Unlock()
                    time.Sleep(10 * time.Millisecond)
                }
            }(w)
        }

        for j := 0; j < totalJobs; j++ {
            jobs <- j
        }
        close(jobs)
        wg.Wait()

        for id, count := range counts {
            fmt.Printf("    worker %d handled %d jobs\n", id, count)
        }
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
With 1 worker, it handles all 20. With 3 and 5, the work distributes roughly evenly.

## Common Mistakes

### Not Closing the Jobs Channel
**Wrong:**
```go
for j := 0; j < 10; j++ {
    jobs <- j
}
// forgot close(jobs)
```
**What happens:** Workers block on `range jobs` forever after all values are consumed. The program deadlocks.

**Fix:** Always close the channel after sending all values.

### Closing the Results Channel Too Early
**Wrong:**
```go
for w := 0; w < 3; w++ {
    go worker(in, results)
}
close(results) // workers haven't finished yet!
```
**What happens:** Workers panic with "send on closed channel".

**Fix:** Use a separate goroutine with `WaitGroup` to close the results channel only after all workers complete.

### Assuming Even Distribution
Go's scheduler does not guarantee round-robin distribution. If one worker is slightly faster, it may grab more jobs. The guarantee is only that each value goes to exactly one receiver.

## Verify What You Learned

Modify the fan-out pipeline to use a configurable number of workers. Run it with 1 worker and 5 workers, timing each. Observe the speedup from parallelizing the processing stage.

## What's Next
Continue to [03-fan-in-merge-results](../03-fan-in-merge-results/03-fan-in-merge-results.md) to learn the complementary pattern: merging multiple channels into one.

## Summary
- Fan-out distributes work from one channel to N goroutines
- Go's channel semantics guarantee each value goes to exactly one receiver
- Workers compete for values -- distribution is natural and non-deterministic
- Use `sync.WaitGroup` to know when all workers have finished
- Close the results channel only after all workers are done (use a separate goroutine)
- Fan-out turns a sequential bottleneck into parallel processing

## Reference
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
- [Advanced Go Concurrency Patterns](https://www.youtube.com/watch?v=QDDwwePbDtw)
