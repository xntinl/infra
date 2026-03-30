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
In a real backend, you often need to process a batch of items in parallel and then summarize the results: validate 50 uploaded files, transform a batch of records, or fan out health checks to multiple services. You need a way to say "wait until all N goroutines have finished" without guessing how long they take.

`sync.WaitGroup` is a counter-based synchronization primitive. You increment the counter with `Add(n)` before launching goroutines, each goroutine decrements it with `Done()` when it finishes, and the main goroutine blocks on `Wait()` until the counter reaches zero. It is the simplest and most common way to synchronize goroutine completion in Go.

The critical rule is: **call `Add` before the `go` statement, not inside the goroutine**. If you call `Add` inside the goroutine, the main goroutine might reach `Wait()` before the goroutine has called `Add`, causing it to return immediately with work still running.

## Step 1 -- Parallel File Processor: Process Each File in Its Own Goroutine

Imagine a batch job that processes uploaded files: validate the format, transform the content, and write the output. Each file is independent, so they can run in parallel:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type FileResult struct {
	Name    string
	Status  string
	Size    int
	Elapsed time.Duration
}

func processFile(name string) FileResult {
	start := time.Now()
	// Simulate variable processing time (50-200ms)
	processingTime := time.Duration(50+rand.Intn(150)) * time.Millisecond
	time.Sleep(processingTime)

	return FileResult{
		Name:    name,
		Status:  "ok",
		Size:    rand.Intn(10000) + 100,
		Elapsed: time.Since(start),
	}
}

func main() {
	files := []string{
		"report-2024-q1.csv",
		"users-export.json",
		"transactions-march.parquet",
		"inventory-snapshot.csv",
		"audit-log-2024.json",
	}

	results := make([]FileResult, len(files))
	var wg sync.WaitGroup

	start := time.Now()

	for i, file := range files {
		wg.Add(1) // CORRECT: Add before the go statement
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = processFile(name) // each goroutine writes to its own index -- safe
		}(i, file)
	}

	wg.Wait() // blocks until all files are processed
	totalElapsed := time.Since(start)

	fmt.Println("=== File Processing Summary ===")
	for _, r := range results {
		fmt.Printf("  %-35s %s  %5d bytes  (%v)\n", r.Name, r.Status, r.Size, r.Elapsed.Round(time.Millisecond))
	}
	fmt.Printf("\nProcessed %d files in %v (parallel)\n", len(files), totalElapsed.Round(time.Millisecond))

	// Compare with sequential time
	sequentialTime := time.Duration(0)
	for _, r := range results {
		sequentialTime += r.Elapsed
	}
	fmt.Printf("Sequential would have taken ~%v\n", sequentialTime.Round(time.Millisecond))
}
```

Expected output:
```
=== File Processing Summary ===
  report-2024-q1.csv                 ok   4231 bytes  (150ms)
  users-export.json                  ok   8712 bytes  (87ms)
  transactions-march.parquet         ok   1055 bytes  (192ms)
  inventory-snapshot.csv             ok   6421 bytes  (63ms)
  audit-log-2024.json                ok   3210 bytes  (134ms)

Processed 5 files in 195ms (parallel)
Sequential would have taken ~626ms
```

Note: each goroutine writes to a unique index in the results slice, so no mutex is needed. This is a common and safe pattern.

### Intermediate Verification
```bash
go run main.go
```
All 5 files should be processed, and the parallel time should be close to the slowest individual file (not the sum of all).

## Step 2 -- Add Before Go, Not Inside

The correct pattern is `wg.Add(1)` before the `go` statement. This simulates a real batch validation pipeline:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var wg sync.WaitGroup
	tasks := []string{
		"validate-schema",
		"check-duplicates",
		"verify-checksums",
		"scan-malware",
	}

	for _, task := range tasks {
		wg.Add(1) // CORRECT: Add before the go statement
		go func(name string) {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond) // simulate work
			fmt.Printf("  [done] %s\n", name)
		}(task)
	}

	wg.Wait()
	fmt.Println("All validation tasks completed. Safe to proceed with import.")
}
```

Expected output:
```
  [done] validate-schema
  [done] check-duplicates
  [done] verify-checksums
  [done] scan-malware
All validation tasks completed. Safe to proceed with import.
```

### Intermediate Verification
```bash
go run main.go
```
All four tasks should print their completion message before the final "Safe to proceed" line.

## Step 3 -- Batch Add for Known Count

When you know the exact number of goroutines upfront, you can call `Add` once. This is common in data processing where you split work into fixed chunks:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	// Simulate processing 10 file chunks in parallel
	const numChunks = 10
	var wg sync.WaitGroup
	chunkResults := make([]int, numChunks)

	wg.Add(numChunks) // add all at once -- we know the count
	for i := 0; i < numChunks; i++ {
		go func(chunkID int) {
			defer wg.Done()
			// Simulate: count "valid records" in this chunk
			chunkResults[chunkID] = (chunkID + 1) * 100
		}(i)
	}

	wg.Wait()

	totalRecords := 0
	for _, count := range chunkResults {
		totalRecords += count
	}
	fmt.Printf("Chunk results: %v\n", chunkResults)
	fmt.Printf("Total valid records across all chunks: %d\n", totalRecords)
}
```

Expected output:
```
Chunk results: [100 200 300 400 500 600 700 800 900 1000]
Total valid records across all chunks: 5500
```

### Intermediate Verification
```bash
go run main.go
```
Results should contain each chunk's count and the total should be 5500.

## Step 4 -- Full Pipeline: Process, Collect Errors, Print Summary

A realistic file processor needs to handle errors. Each goroutine reports success or failure, and the main goroutine summarizes:

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ProcessResult struct {
	FileName string
	Success  bool
	Error    string
	Records  int
	Duration time.Duration
}

func processFileWithErrors(name string) ProcessResult {
	start := time.Now()
	time.Sleep(time.Duration(30+rand.Intn(100)) * time.Millisecond)

	// Simulate 20% failure rate
	if rand.Float64() < 0.2 {
		return ProcessResult{
			FileName: name,
			Success:  false,
			Error:    "invalid header: expected 5 columns, got 3",
			Duration: time.Since(start),
		}
	}

	return ProcessResult{
		FileName: name,
		Success:  true,
		Records:  rand.Intn(5000) + 500,
		Duration: time.Since(start),
	}
}

func main() {
	files := []string{
		"batch-001.csv", "batch-002.csv", "batch-003.csv",
		"batch-004.csv", "batch-005.csv", "batch-006.csv",
		"batch-007.csv", "batch-008.csv", "batch-009.csv",
		"batch-010.csv",
	}

	results := make([]ProcessResult, len(files))
	var wg sync.WaitGroup

	start := time.Now()

	wg.Add(len(files))
	for i, file := range files {
		go func(idx int, name string) {
			defer wg.Done()
			results[idx] = processFileWithErrors(name)
		}(i, file)
	}

	wg.Wait()
	totalElapsed := time.Since(start)

	// Summary
	succeeded := 0
	failed := 0
	totalRecords := 0

	fmt.Println("=== Batch Processing Report ===")
	for _, r := range results {
		if r.Success {
			succeeded++
			totalRecords += r.Records
			fmt.Printf("  OK   %-15s  %4d records  (%v)\n", r.FileName, r.Records, r.Duration.Round(time.Millisecond))
		} else {
			failed++
			fmt.Printf("  FAIL %-15s  %s  (%v)\n", r.FileName, r.Error, r.Duration.Round(time.Millisecond))
		}
	}

	fmt.Printf("\nCompleted in %v: %d succeeded, %d failed, %d total records\n",
		totalElapsed.Round(time.Millisecond), succeeded, failed, totalRecords)
}
```

Expected output:
```
=== Batch Processing Report ===
  OK   batch-001.csv    2341 records  (78ms)
  OK   batch-002.csv    4102 records  (45ms)
  FAIL batch-003.csv    invalid header: expected 5 columns, got 3  (92ms)
  OK   batch-004.csv    1287 records  (51ms)
  ...

Completed in 105ms: 8 succeeded, 2 failed, 19432 total records
```

### Intermediate Verification
```bash
go run main.go
```
All 10 files should be processed. Some will succeed and some fail. The total time should be near the slowest individual file.

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

**What happens:** `Wait()` can return before all goroutines have called `Add`, so some goroutines may not be waited for. In a file processor, this means some files appear to have been processed when they have not.

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
		if true {
			return // early return -- Done is never called!
		}
		wg.Done()
	}()
	wg.Wait() // blocks forever -- deadlock
}
```

**What happens:** Deadlock. The counter never reaches zero. Your batch job hangs indefinitely.

**Fix:** Use `defer wg.Done()` so it runs regardless of how the goroutine exits.

### Passing WaitGroup by Value

```go
package main

import (
	"fmt"
	"sync"
)

func processFile(wg sync.WaitGroup, name string) { // receives a COPY
	defer wg.Done() // decrements the copy, not the original
	fmt.Printf("Processed %s\n", name)
}

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go processFile(wg, fmt.Sprintf("file-%d.csv", i)) // passes by value
	}
	wg.Wait() // blocks forever -- deadlock
}
```

**What happens:** The original WaitGroup counter is never decremented. Deadlock.

**Fix:** Pass `*sync.WaitGroup` (pointer):
```go
func processFile(wg *sync.WaitGroup, name string) {
	defer wg.Done()
	fmt.Printf("Processed %s\n", name)
}
```

## Verify What You Learned

Build a parallel health checker that pings 10 services (simulated with random latency and 30% failure rate). Use WaitGroup to wait for all checks. Print a summary showing which services are healthy, which are down, and the total check duration. Verify with `-race` that there are no data races.

## What's Next
Continue to [04-once-singleton-init](../04-once-singleton-init/04-once-singleton-init.md) to learn how `sync.Once` ensures code runs exactly once, even under concurrent access.

## Summary
- `sync.WaitGroup` is a counter: `Add` increments, `Done` decrements, `Wait` blocks until zero
- Always call `Add` before the `go` statement, never inside the goroutine
- Use `defer wg.Done()` to guarantee the counter is decremented on all exit paths
- Pass WaitGroup by pointer (`*sync.WaitGroup`), never by value
- For a known count, call `Add(n)` once before the loop
- Each goroutine writing to its own slice index is safe without a mutex
- WaitGroup replaces fragile `time.Sleep` synchronization with deterministic completion waiting

## Reference
- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [Go by Example: WaitGroups](https://gobyexample.com/waitgroups)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
