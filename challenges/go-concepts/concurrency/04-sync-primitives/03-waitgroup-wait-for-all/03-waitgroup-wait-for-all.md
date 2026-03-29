---
difficulty: basic
concepts: [sync.WaitGroup, Add, Done, Wait, goroutine synchronization]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, go keyword]
---

# 3. WaitGroup: Wait for All


## Learning Objectives
After completing this exercise, you will be able to:
- **Use** `sync.WaitGroup` to wait for multiple goroutines to complete
- **Apply** the correct pattern: `Add` before `go`, `Done` inside the goroutine
- **Identify** common mistakes such as calling `Add` inside the goroutine or producing a negative counter

## Why WaitGroup
In the goroutines exercises, you used `time.Sleep` to wait for goroutines to finish. This is fragile: sleep too little and goroutines are killed mid-execution; sleep too much and you waste time. You need a way to say "wait until all N goroutines have finished" without guessing how long they take.

`sync.WaitGroup` is a counter-based synchronization primitive. You increment the counter with `Add(n)` before launching goroutines, each goroutine decrements it with `Done()` when it finishes, and the main goroutine blocks on `Wait()` until the counter reaches zero. It is the simplest and most common way to synchronize goroutine completion in Go.

The critical rule is: **call `Add` before the `go` statement, not inside the goroutine**. If you call `Add` inside the goroutine, the main goroutine might reach `Wait()` before the goroutine has called `Add`, causing it to return immediately with work still running.

## Step 1 -- Replace time.Sleep with WaitGroup

Run `main.go` and compare the fragile sleep-based approach with the reliable WaitGroup:

```bash
go run main.go
```

The WaitGroup version finishes in exactly the time of the slowest worker (~500ms), while the sleep version always waits a fixed 600ms regardless.

### Intermediate Verification
All 5 workers should finish, and the WaitGroup version reports the actual total time.

## Step 2 -- Add Before Go, Not Inside

The `addBeforeGo` function shows the correct pattern:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	tasks := []string{"fetch-users", "fetch-orders", "fetch-products"}

	for _, task := range tasks {
		wg.Add(1) // CORRECT: Add before the go statement
		go func(name string) {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			fmt.Printf("Task %q completed\n", name)
		}(task)
	}

	wg.Wait()
	fmt.Println("All tasks completed.")
}
```

Expected output:
```
Task "fetch-users" completed
Task "fetch-orders" completed
Task "fetch-products" completed
All tasks completed.
```

### Intermediate Verification
```bash
go run main.go
```
All three tasks should print their completion message before "All tasks completed."

## Step 3 -- Batch Add for Known Count

When you know the number of goroutines upfront, you can call `Add` once:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	const numWorkers = 10
	var wg sync.WaitGroup
	results := make([]int, numWorkers)

	wg.Add(numWorkers) // add all at once
	for i := 0; i < numWorkers; i++ {
		go func(id int) {
			defer wg.Done()
			results[id] = id * id // each goroutine writes to its own index -- safe
		}(i)
	}

	wg.Wait()
	fmt.Printf("Results: %v\n", results)
}
```

Expected output:
```
Results: [0 1 4 9 16 25 36 49 64 81]
```

Note: each goroutine writes to a unique index in the slice, so no mutex is needed. This is a common and safe pattern.

### Intermediate Verification
```bash
go run main.go
```
Results should contain the squares: `[0 1 4 9 16 25 36 49 64 81]`.

## Step 4 -- Parallel Sum

The `parallelSum` function splits computation across 10 goroutines:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	const size = 1_000_000
	numbers := make([]int, size)
	for i := range numbers {
		numbers[i] = i
	}

	// Sequential sum for verification
	sequentialSum := int64(0)
	for _, n := range numbers {
		sequentialSum += int64(n)
	}

	// Parallel sum: 10 chunks
	const numWorkers = 10
	chunkSize := size / numWorkers
	partialSums := make([]int64, numWorkers)
	var wg sync.WaitGroup

	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()
			start := workerID * chunkSize
			end := start + chunkSize

			var sum int64
			for _, n := range numbers[start:end] {
				sum += int64(n)
			}
			partialSums[workerID] = sum
		}(i)
	}

	wg.Wait()

	parallelTotal := int64(0)
	for _, s := range partialSums {
		parallelTotal += s
	}

	fmt.Printf("Sequential: %d\n", sequentialSum)
	fmt.Printf("Parallel:   %d\n", parallelTotal)
	fmt.Printf("Match: %v\n", sequentialSum == parallelTotal)
}
```

Expected output:
```
Sequential: 499999500000
Parallel:   499999500000
Match: true
```

### Intermediate Verification
```bash
go run main.go
```
Both sums should match perfectly.

## Common Mistakes

### Add Inside the Goroutine

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		go func(id int) {
			wg.Add(1) // RACE: main might reach Wait() before this executes
			defer wg.Done()
			fmt.Println(id)
		}(i)
	}
	wg.Wait() // might return immediately with goroutines still running
	fmt.Println("done -- but some goroutines may have been skipped!")
}
```

**What happens:** `Wait()` can return before all goroutines have called `Add`, so some goroutines may not be waited for.

**Fix:** Always call `Add` before the `go` statement.

### Negative WaitGroup Counter

```go
package main

import "sync"

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		wg.Done() // panic: sync: negative WaitGroup counter
	}()
	wg.Wait()
}
```

**What happens:** Runtime panic. Each goroutine must call `Done` exactly once.

**Fix:** Use `defer wg.Done()` as the first line inside the goroutine to guarantee it is called exactly once.

### Forgetting Done

```go
package main

import "sync"

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		// Some condition causes early return
		if true {
			return // Done is never called!
		}
		wg.Done()
	}()
	wg.Wait() // blocks forever -- deadlock
}
```

**What happens:** Deadlock. The counter never reaches zero.

**Fix:** Use `defer wg.Done()` so it runs regardless of how the goroutine exits.

### Passing WaitGroup by Value

```go
package main

import (
	"fmt"
	"sync"
)

func worker(wg sync.WaitGroup, id int) { // receives a COPY
	defer wg.Done() // decrements the copy, not the original
	fmt.Printf("Worker %d done\n", id)
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go worker(wg, i) // passes by value -- the original never decrements
	}
	wg.Wait() // blocks forever -- deadlock
}
```

**What happens:** The original WaitGroup counter is never decremented. Deadlock.

**Fix:** Pass `*sync.WaitGroup` (pointer):
```go
func worker(wg *sync.WaitGroup, id int) {
	defer wg.Done()
	fmt.Printf("Worker %d done\n", id)
}
```

## Verify What You Learned

Create a `parallelSum` function that:
1. Splits a slice of 1,000,000 integers into 10 chunks
2. Launches a goroutine for each chunk to compute its partial sum
3. Uses WaitGroup to wait for all goroutines
4. Combines the partial sums into a total

Verify the result matches a sequential sum.

## What's Next
Continue to [04-once-singleton-init](../04-once-singleton-init/04-once-singleton-init.md) to learn how `sync.Once` ensures code runs exactly once, even under concurrent access.

## Summary
- `sync.WaitGroup` is a counter: `Add` increments, `Done` decrements, `Wait` blocks until zero
- Always call `Add` before the `go` statement, never inside the goroutine
- Use `defer wg.Done()` to guarantee the counter is decremented on all exit paths
- Pass WaitGroup by pointer (`*sync.WaitGroup`), never by value
- For a known count, call `Add(n)` once before the loop
- WaitGroup replaces fragile `time.Sleep` synchronization with deterministic completion waiting

## Reference
- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [Go by Example: WaitGroups](https://gobyexample.com/waitgroups)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
