# 8. Race-Free Design Patterns

<!--
difficulty: advanced
concepts: [confinement, immutability, ownership, communication, design for concurrency]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [goroutines, channels, sync.WaitGroup, data race concept, race detector, mutex, channels, atomic]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-07 (all data race exercises)
- Solid understanding of goroutines, channels, and synchronization primitives

## Learning Objectives
After completing this exercise, you will be able to:
- **Design** concurrent programs where races are impossible by construction
- **Apply** confinement: each goroutine owns its data exclusively
- **Apply** immutability: pass copies instead of pointers to shared data
- **Apply** communication: use channels as the sole coordination mechanism
- **Combine** all three patterns in a single program

## Why Design Patterns Over Fixes
Exercises 03-05 showed how to fix races after they occur: add a mutex, use a channel, use atomics. These are reactive approaches. This exercise takes the proactive approach: design your concurrent code so that races **cannot happen**.

The principle: **the best race fix is making races impossible by design.**

Three design patterns achieve this:
1. **Confinement** -- each goroutine works only on its own data. No sharing at all.
2. **Immutability** -- shared data is never modified. Read-only access from multiple goroutines is safe.
3. **Communication** -- goroutines exchange data through channels, never through shared variables.

When you combine these patterns, you write concurrent code that is correct by construction, not by careful locking.

## Step 1 -- Confinement: Each Goroutine Owns Its Data

Edit `main.go` and implement `confinementPattern`. Each goroutine works on its own slice, processes it independently, and sends the result through a channel. No goroutine ever touches another's data:

```go
func confinementPattern() {
    fmt.Println("=== Pattern 1: Confinement ===")

    data := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
    numWorkers := 4
    chunkSize := len(data) / numWorkers
    results := make(chan int, numWorkers)

    for w := 0; w < numWorkers; w++ {
        start := w * chunkSize
        end := start + chunkSize
        if w == numWorkers-1 {
            end = len(data) // last worker takes the remainder
        }

        // Each goroutine gets its own slice -- no overlap, no sharing
        chunk := data[start:end]
        go func(myData []int) {
            sum := 0
            for _, v := range myData {
                sum += v
            }
            results <- sum
        }(chunk)
    }

    total := 0
    for w := 0; w < numWorkers; w++ {
        total += <-results
    }

    fmt.Printf("  Sum of %v = %d (computed by %d confined workers)\n", data, total, numWorkers)
}
```

Key insight: each goroutine processes `myData`, which is its own slice of the original data. No goroutine reads or writes another goroutine's slice. The only shared communication is through the `results` channel.

### Intermediate Verification
```bash
go run -race main.go
```
Expected: correct sum (78), no race warnings.

## Step 2 -- Immutability: Pass Copies, Not Pointers

Implement `immutabilityPattern`. Create a configuration struct that is passed by value (copied) to each goroutine. Since each goroutine gets its own copy, modifications do not affect others:

```go
type Config struct {
    WorkerID   int
    Multiplier int
    Label      string
}

func immutabilityPattern() {
    fmt.Println("\n=== Pattern 2: Immutability ===")

    baseConfig := Config{Multiplier: 10, Label: "worker"}
    results := make(chan string, 5)

    for i := 0; i < 5; i++ {
        // Copy the config and customize for this worker
        cfg := baseConfig   // VALUE COPY -- each goroutine gets its own
        cfg.WorkerID = i
        cfg.Label = fmt.Sprintf("worker-%d", i)

        go func(c Config) { // passed by value: another copy
            result := fmt.Sprintf("  %s: %d * %d = %d",
                c.Label, c.WorkerID, c.Multiplier, c.WorkerID*c.Multiplier)
            results <- result
        }(cfg)
    }

    for i := 0; i < 5; i++ {
        fmt.Println(<-results)
    }
}
```

Key insight: `cfg := baseConfig` creates a value copy. Passing `cfg` by value to the goroutine creates another copy. The goroutine's `c` is completely independent. Even if it modified `c.Multiplier`, it would not affect any other goroutine.

### Intermediate Verification
```bash
go run -race main.go
```
Expected: five lines with correct multiplications, no race warnings.

## Step 3 -- Communication: Channels as Sole Coordination

Implement `communicationPattern`. Build a pipeline where each stage is a goroutine, and data flows exclusively through channels. No goroutine has access to another's internal state:

```go
func communicationPattern() {
    fmt.Println("\n=== Pattern 3: Communication (Pipeline) ===")

    // Stage 1: generate numbers
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

    // Stage 2: square each number
    square := func(in <-chan int) <-chan int {
        out := make(chan int)
        go func() {
            defer close(out)
            for n := range in {
                out <- n * n
            }
        }()
        return out
    }

    // Stage 3: format as string
    format := func(in <-chan int) <-chan string {
        out := make(chan string)
        go func() {
            defer close(out)
            for n := range in {
                out <- fmt.Sprintf("  %d", n)
            }
        }()
        return out
    }

    // Connect the pipeline
    numbers := generate()
    squared := square(numbers)
    formatted := format(squared)

    // Consume results
    for s := range formatted {
        fmt.Println(s)
    }
}
```

Key insight: each stage function is a self-contained goroutine. The only way data moves between stages is through channels. There are no shared variables, no mutexes, and no possibility of a data race.

### Intermediate Verification
```bash
go run -race main.go
```
Expected: 1, 4, 9, 16, 25 printed (one per line), no race warnings.

## Step 4 -- Combine All Three Patterns

Implement `combinedPattern` that uses all three patterns together to process a dataset:

```go
func combinedPattern() {
    fmt.Println("\n=== Combined: All Three Patterns ===")

    type Task struct {
        ID    int
        Input []int
    }

    type Result struct {
        TaskID int
        Sum    int
    }

    // Immutability: create task configs as values
    tasks := []Task{
        {ID: 1, Input: []int{1, 2, 3}},
        {ID: 2, Input: []int{4, 5, 6}},
        {ID: 3, Input: []int{7, 8, 9}},
        {ID: 4, Input: []int{10, 11, 12}},
    }

    // Communication: channels are the sole coordination mechanism
    resultCh := make(chan Result, len(tasks))

    var wg sync.WaitGroup
    for _, t := range tasks {
        wg.Add(1)
        task := t // copy the task value (immutability)

        // Confinement: each goroutine works only on its own task
        go func(myTask Task) {
            defer wg.Done()
            sum := 0
            for _, v := range myTask.Input {
                sum += v
            }
            // Communication: send result through channel
            resultCh <- Result{TaskID: myTask.ID, Sum: sum}
        }(task)
    }

    // Wait and close
    go func() {
        wg.Wait()
        close(resultCh)
    }()

    // Collect results
    totalSum := 0
    for r := range resultCh {
        fmt.Printf("  Task %d: sum = %d\n", r.TaskID, r.Sum)
        totalSum += r.Sum
    }
    fmt.Printf("  Total sum: %d\n", totalSum)
}
```

This function demonstrates:
- **Confinement**: each goroutine processes only `myTask`, its own copy
- **Immutability**: `task := t` creates a copy; `myTask Task` parameter creates another
- **Communication**: `resultCh` is the only way results flow back to the collector

### Intermediate Verification
```bash
go run -race main.go
```
Expected: all four task sums printed, total sum = 78, no race warnings.

## Common Mistakes

### Sharing Slices by Accident
**Wrong:**
```go
data := []int{1, 2, 3, 4, 5}
go func() {
    data[0] = 99 // modifies the underlying array
}()
fmt.Println(data[0]) // race: reads the same array
```
Slices contain a pointer to the underlying array. Passing a slice to a goroutine does not copy the elements. Use `copy()` or pass the specific sub-slice that only that goroutine will use.

### Thinking Structs Are Always Copied
Structs are copied by value when assigned or passed as parameters. However, if a struct contains pointer fields, slices, or maps, only the reference is copied, not the underlying data. The goroutine still shares the underlying data.

### Over-Engineering With Channels
Not every concurrent program needs channels. If the problem is naturally parallel with no communication (like the confinement pattern), channels add unnecessary complexity. Use the simplest pattern that fits.

## Verify What You Learned

Run the full program:
```bash
go run -race main.go
```

Confirm:
1. All four sections produce correct output
2. Zero race warnings from the race detector
3. No mutexes or atomics were needed

Answer these questions:
1. What is the difference between confinement and immutability?
2. When does the communication pattern (channels) add value over simple confinement?
3. Why is "design for no races" better than "fix races with locks"?

## What's Next
You have completed the data races section. You now have a complete toolkit:
- Detect races with `-race`
- Fix races with mutexes, channels, or atomics
- Design code where races cannot occur

Apply these patterns in your own concurrent programs. When writing new concurrent code, start with the question: "How can I design this so races are impossible?"

## Summary
- **Confinement**: each goroutine owns its data exclusively; no sharing, no races
- **Immutability**: pass copies (by value) so goroutines cannot interfere with each other
- **Communication**: use channels as the sole coordination mechanism between goroutines
- Combining all three patterns yields concurrent code that is correct by construction
- Design for no races is better than fixing races after the fact
- `go run -race` is your verification tool: zero warnings confirms the design works
- The best race fix is making races impossible by design

## Reference
- [Effective Go: Share by Communicating](https://go.dev/doc/effective_go#sharing)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)
- [Go Proverbs](https://go-proverbs.github.io/)
