---
difficulty: advanced
concepts: [confinement, immutability, ownership, communication, pipeline, fan-out, fan-in]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [goroutines, channels, sync.WaitGroup, data race concept, race detector, mutex, channels, atomic]
---

# 8. Race-Free Design Patterns


## Learning Objectives
After completing this exercise, you will be able to:
- **Design** concurrent programs where races are impossible by construction
- **Apply** confinement: each goroutine owns its data exclusively
- **Apply** immutability: pass copies instead of pointers to shared data
- **Apply** communication: use channels as the sole coordination mechanism
- **Build** fan-out/fan-in patterns using these principles
- **Combine** all three patterns in a single program

## Why Design Patterns Over Fixes

Exercises 03-05 showed how to **fix** races after they occur: add a mutex, use a channel, use atomics. These are reactive approaches. This exercise takes the proactive approach: design your concurrent code so that races **cannot happen**.

The principle: **the best race fix is making races impossible by design.**

Three design patterns achieve this:

| Pattern | Mechanism | No Race Because |
|---------|-----------|-----------------|
| **Confinement** | Each goroutine works on its own data | No sharing at all |
| **Immutability** | Shared data is never modified | Read-only access is safe |
| **Communication** | Data flows through channels | No shared variables |

When you combine these patterns, you write concurrent code that is **correct by construction**, not by careful locking.

## Step 1 -- Confinement: Each Goroutine Owns Its Data

Divide a dataset into non-overlapping chunks. Each goroutine processes exclusively its own chunk:

```go
package main

import "fmt"

func confinementPattern() {
    data := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
    numWorkers := 4
    chunkSize := len(data) / numWorkers

    type workerResult struct {
        workerID int
        chunk    []int
        sum      int
    }

    results := make(chan workerResult, numWorkers)

    for w := 0; w < numWorkers; w++ {
        start := w * chunkSize
        end := start + chunkSize
        if w == numWorkers-1 {
            end = len(data)
        }

        chunk := data[start:end]
        go func(id int, myData []int) {
            sum := 0
            for _, v := range myData {
                sum += v
            }
            results <- workerResult{workerID: id, chunk: myData, sum: sum}
        }(w, chunk)
    }

    total := 0
    for w := 0; w < numWorkers; w++ {
        r := <-results
        fmt.Printf("  Worker %d: sum of %v = %d\n", r.workerID, r.chunk, r.sum)
        total += r.sum
    }
    fmt.Printf("Total: %d (expected 78)\n", total)
}

func main() {
    confinementPattern()
}
```

Key insight: each goroutine processes `myData`, which is its own non-overlapping slice. No goroutine reads or writes another goroutine's data. The only shared communication is through the `results` channel.

### Verification
```bash
go run -race main.go
```
Expected: correct sum (78), zero race warnings.

## Step 2 -- Immutability: Pass Copies, Not Pointers

Pass configuration structs by value. Each goroutine gets its own copy:

```go
package main

import "fmt"

type Config struct {
    WorkerID   int
    Multiplier int
    Label      string
}

func immutabilityPattern() {
    baseConfig := Config{Multiplier: 10, Label: "worker"}
    results := make(chan string, 5)

    for i := 0; i < 5; i++ {
        cfg := baseConfig         // VALUE COPY
        cfg.WorkerID = i
        cfg.Label = fmt.Sprintf("worker-%d", i)

        go func(c Config) {       // passed by value: another copy
            result := fmt.Sprintf("  %s: %d * %d = %d",
                c.Label, c.WorkerID, c.Multiplier, c.WorkerID*c.Multiplier)
            results <- result
        }(cfg)
    }

    for i := 0; i < 5; i++ {
        fmt.Println(<-results)
    }
}

func main() {
    immutabilityPattern()
}
```

Key insight: `cfg := baseConfig` creates a value copy. Passing `cfg` by value to the goroutine creates another copy. The goroutine's `c` is completely independent. Even if it modified `c.Multiplier`, no other goroutine would see it.

**Warning**: if a struct contains pointer fields, slices, or maps, only the reference is copied, not the underlying data. The goroutine still shares the underlying data. Use `copy()` for slices or design structs with only value types.

### Verification
```bash
go run -race main.go
```
Expected: five lines with correct multiplications, zero race warnings.

## Step 3 -- Communication: Channels as Sole Coordination

Build a pipeline where each stage is a goroutine, and data flows exclusively through channels:

```go
package main

import "fmt"

func communicationPattern() {
    // Stage 1: generate numbers.
    generate := func() <-chan int {
        out := make(chan int)
        go func() {
            defer close(out)
            for i := 1; i <= 5; i++ {
                out <- i
            }
        }()
        return out
    }

    // Stage 2: square each number.
    square := func(in <-chan int) <-chan [2]int {
        out := make(chan [2]int)
        go func() {
            defer close(out)
            for n := range in {
                out <- [2]int{n, n * n}
            }
        }()
        return out
    }

    // Connect and consume.
    numbers := generate()
    squared := square(numbers)

    for pair := range squared {
        fmt.Printf("  %d -> %d\n", pair[0], pair[1])
    }
}

func main() {
    communicationPattern()
}
```

Key insight: each stage function is a self-contained goroutine. The only way data moves between stages is through channels. There are no shared variables, no mutexes, and no possibility of a data race.

### Verification
```bash
go run -race main.go
```
Expected: 1->1, 2->4, 3->9, 4->16, 5->25, zero race warnings.

## Step 4 -- Combined: All Three Patterns

The `main.go` includes `combinedPattern()` that uses all three patterns together:

```go
package main

import (
    "fmt"
    "sync"
)

func combinedPattern() {
    type Task struct {
        ID    int
        Input []int
    }

    type Result struct {
        TaskID int
        Input  []int
        Sum    int
    }

    tasks := []Task{
        {ID: 1, Input: []int{1, 2, 3}},
        {ID: 2, Input: []int{4, 5, 6}},
        {ID: 3, Input: []int{7, 8, 9}},
        {ID: 4, Input: []int{10, 11, 12}},
    }

    resultCh := make(chan Result, len(tasks))

    var wg sync.WaitGroup
    for _, t := range tasks {
        wg.Add(1)
        task := t // value copy (immutability)

        // Confinement: each goroutine works exclusively on myTask.
        go func(myTask Task) {
            defer wg.Done()
            sum := 0
            for _, v := range myTask.Input {
                sum += v
            }
            // Communication: send result, don't share state.
            resultCh <- Result{TaskID: myTask.ID, Input: myTask.Input, Sum: sum}
        }(task)
    }

    go func() {
        wg.Wait()
        close(resultCh)
    }()

    totalSum := 0
    for r := range resultCh {
        fmt.Printf("  Task %d: sum(%v) = %d\n", r.TaskID, r.Input, r.Sum)
        totalSum += r.Sum
    }
    fmt.Printf("Total: %d (expected 78)\n", totalSum)
}

func main() {
    combinedPattern()
}
```

This function demonstrates:
- **Confinement**: each goroutine processes only `myTask`, its own copy
- **Immutability**: `task := t` creates a copy; `myTask Task` parameter creates another
- **Communication**: `resultCh` is the only way results flow back to the collector

### Verification
```bash
go run -race main.go
```
Expected: all four task sums, total = 78, zero race warnings.

## Step 5 -- Fan-Out / Fan-In

The `main.go` also includes `fanOutFanIn()`: distribute work across multiple workers (fan-out) and collect results through a single channel (fan-in):

```go
package main

import (
    "fmt"
    "sync"
)

func fanOutFanIn() {
    jobs := make(chan int, 10)
    results := make(chan string, 10)

    numWorkers := 3

    // Fan-out: launch workers.
    var wg sync.WaitGroup
    for w := 0; w < numWorkers; w++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for job := range jobs {
                result := fmt.Sprintf("  worker processed: %d -> %d", job, job*2)
                results <- result
            }
        }()
    }

    // Send jobs.
    go func() {
        for i := 1; i <= 10; i++ {
            jobs <- i
        }
        close(jobs)
    }()

    // Fan-in: close results after workers finish.
    go func() {
        wg.Wait()
        close(results)
    }()

    count := 0
    for r := range results {
        fmt.Println(r)
        count++
    }
    fmt.Printf("Processed %d items with %d workers.\n", count, numWorkers)
}

func main() {
    fanOutFanIn()
}
```

Each job is consumed by exactly one worker (the channel guarantees this). Each worker processes its job independently (confinement). Results flow through a single channel (communication).

### Verification
```bash
go run -race main.go
```
Expected: 10 items processed, zero race warnings.

## Common Mistakes

### Sharing Slices by Accident
```go
data := []int{1, 2, 3, 4, 5}
go func() {
    data[0] = 99 // modifies the underlying array
}()
fmt.Println(data[0]) // race: reads the same array
```
Slices contain a pointer to the underlying array. Passing a slice to a goroutine does NOT copy the elements. Use `copy()` or pass non-overlapping sub-slices.

### Thinking Structs Are Always Copied
Structs are copied by value when assigned or passed as parameters. However, if a struct contains **pointer fields, slices, or maps**, only the reference is copied, not the underlying data. The goroutine still shares the underlying data.

### Over-Engineering With Channels
Not every concurrent program needs channels. If the problem is naturally parallel with no communication (like the confinement pattern), channels add unnecessary complexity. Use the simplest pattern that fits.

## Design Decision Flowchart

1. **Can each goroutine work on its own data?** -> Confinement (simplest)
2. **Is the data read-only after creation?** -> Immutability (pass by value)
3. **Do goroutines need to coordinate?** -> Communication (channels)
4. **Is the shared state a simple counter?** -> `sync/atomic` (exercise 05)
5. **Is the shared state complex?** -> `sync.Mutex` (exercise 03)

Always prefer design-level solutions (1-3) over fix-level solutions (4-5).

## Verify What You Learned

```bash
go run -race main.go
```

Confirm:
1. All five patterns produce correct output
2. Zero race warnings from the race detector
3. No mutexes or atomics were needed

Answer these questions:
1. What is the difference between confinement and immutability?
2. When does the communication pattern add value over simple confinement?
3. Why is "design for no races" better than "fix races with locks"?

## What's Next

You have completed the data races section. You now have a complete toolkit:

| Skill | Exercise |
|-------|----------|
| See the problem | 01 - Your First Data Race |
| Detect automatically | 02 - Race Detector Flag |
| Fix with mutex | 03 - Fix Race with Mutex |
| Fix with channel | 04 - Fix Race with Channel |
| Fix with atomic | 05 - Fix Race with Atomic |
| Handle map races | 06 - Subtle Race: Map Access |
| Avoid closure bugs | 07 - Race in Closure Loops |
| Design away races | 08 - Race-Free Design Patterns |

Apply these patterns in your own concurrent programs. When writing new concurrent code, start with the question: **"How can I design this so races are impossible?"**

## Summary
- **Confinement**: each goroutine owns its data exclusively; no sharing, no races
- **Immutability**: pass copies (by value) so goroutines cannot interfere with each other
- **Communication**: use channels as the sole coordination mechanism between goroutines
- **Fan-out/fan-in**: distribute work across workers via channels, collect results through a single channel
- Combining all three patterns yields concurrent code that is **correct by construction**
- Design for no races is better than fixing races after the fact
- `go run -race` is your verification tool: zero warnings confirms the design works
- **The best race fix is making races impossible by design**

## Reference
- [Effective Go: Share by Communicating](https://go.dev/doc/effective_go#sharing)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)
- [Go Proverbs](https://go-proverbs.github.io/)
