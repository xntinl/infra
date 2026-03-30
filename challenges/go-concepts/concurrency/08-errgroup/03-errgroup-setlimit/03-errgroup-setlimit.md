---
difficulty: intermediate
concepts: [semaphore channel, bounded concurrency, backpressure, file descriptor limits, resource exhaustion]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 3. Errgroup SetLimit -- Bulk File Validator

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a bulk file validator that limits concurrency to avoid exhausting file descriptors
- **Implement** the semaphore pattern using a buffered channel to cap concurrent goroutines
- **Explain** what backpressure is and why `g.Go()` blocking is desirable behavior
- **Combine** bounded concurrency with context cancellation for production-grade file processing

## Why Bounded Concurrency

Your CI pipeline validates configuration files before deployment: check JSON syntax, verify required fields, ensure values are within bounds. You have 100+ config files. Launching 100 goroutines that each open a file simultaneously can exceed the OS file descriptor limit (often 1024 on Linux). The result: `open /path/to/file: too many open files`.

Even without hitting OS limits, unbounded concurrency wastes resources. If you are validating files on a shared NFS mount, 100 concurrent reads may saturate the I/O bus and slow everything down. Limiting to 5-10 concurrent validations keeps throughput high without resource exhaustion.

The semaphore pattern solves this: a buffered channel of capacity N acts as a token bucket. A goroutine acquires a token before starting work and releases it when done. If all tokens are taken, the next goroutine blocks until one becomes available. This is natural backpressure.

## Step 1 -- Unbounded Concurrency (The Problem)

Observe what happens when 20 file validations run simultaneously with no limit:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	files := generateFileList(20)

	fmt.Println("=== Unbounded Concurrency ===")
	start := time.Now()

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	activeCount := 0
	var mu sync.Mutex
	peakConcurrent := 0

	for _, f := range files {
		f := f
		wg.Add(1)
		go func() {
			defer wg.Done()

			mu.Lock()
			activeCount++
			if activeCount > peakConcurrent {
				peakConcurrent = activeCount
			}
			current := activeCount
			mu.Unlock()

			fmt.Printf("  [%v] validating %s (concurrent: %d)\n",
				time.Since(start).Round(time.Millisecond), f, current)

			if err := validateFile(f); err != nil {
				once.Do(func() { firstErr = err })
			}

			mu.Lock()
			activeCount--
			mu.Unlock()
		}()
	}

	wg.Wait()
	fmt.Printf("\nPeak concurrent goroutines: %d\n", peakConcurrent)
	fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	}
}

func generateFileList(n int) []string {
	files := make([]string, n)
	for i := 0; i < n; i++ {
		files[i] = fmt.Sprintf("config/service-%02d.json", i)
	}
	return files
}

func validateFile(path string) error {
	time.Sleep(100 * time.Millisecond) // simulate file I/O + parsing
	if path == "config/service-07.json" {
		return fmt.Errorf("validate %s: missing required field 'port'", path)
	}
	return nil
}
```

**Expected output:**
```
=== Unbounded Concurrency ===
  [0ms] validating config/service-00.json (concurrent: 1)
  [0ms] validating config/service-01.json (concurrent: 2)
  ...all 20 start at ~0ms...
  [0ms] validating config/service-19.json (concurrent: 20)

Peak concurrent goroutines: 20
Total time: 100ms
First error: validate config/service-07.json: missing required field 'port'
```

All 20 goroutines start simultaneously. With real file I/O on 1000 files, this would open 1000 file descriptors at once. On most systems, `ulimit -n` is 1024 -- you would get `too many open files` errors.

## Step 2 -- Semaphore Channel (The Solution)

Add a buffered channel as a semaphore. Each goroutine must acquire a token (send to the channel) before starting and release it (receive from the channel) when done:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	files := generateFileList(20)

	fmt.Println("=== Bounded Concurrency (semaphore, limit=5) ===")
	start := time.Now()

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	var mu sync.Mutex
	activeCount := 0
	peakConcurrent := 0

	for _, f := range files {
		f := f

		sem <- struct{}{} // ACQUIRE: blocks if 5 goroutines are already running

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // RELEASE: free the slot when done

			mu.Lock()
			activeCount++
			if activeCount > peakConcurrent {
				peakConcurrent = activeCount
			}
			mu.Unlock()

			if err := validateFile(f); err != nil {
				once.Do(func() { firstErr = err })
			} else {
				fmt.Printf("  [%v] validated %s\n",
					time.Since(start).Round(time.Millisecond), f)
			}

			mu.Lock()
			activeCount--
			mu.Unlock()
		}()
	}

	wg.Wait()
	fmt.Printf("\nPeak concurrent goroutines: %d\n", peakConcurrent)
	fmt.Printf("Total time: %v (ceil(20/5) batches of ~100ms)\n",
		time.Since(start).Round(time.Millisecond))
	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	}
}

func generateFileList(n int) []string {
	files := make([]string, n)
	for i := 0; i < n; i++ {
		files[i] = fmt.Sprintf("config/service-%02d.json", i)
	}
	return files
}

func validateFile(path string) error {
	time.Sleep(100 * time.Millisecond)
	if path == "config/service-07.json" {
		return fmt.Errorf("validate %s: missing required field 'port'", path)
	}
	return nil
}
```

**Expected output:**
```
=== Bounded Concurrency (semaphore, limit=5) ===
  [100ms] validated config/service-00.json
  [100ms] validated config/service-01.json
  [100ms] validated config/service-02.json
  [100ms] validated config/service-03.json
  [100ms] validated config/service-04.json
  [200ms] validated config/service-05.json
  [200ms] validated config/service-06.json
  ...
  [400ms] validated config/service-19.json

Peak concurrent goroutines: 5
Total time: 400ms (ceil(20/5) batches of ~100ms)
First error: validate config/service-07.json: missing required field 'port'
```

With limit 5 and 20 files of 100ms each: `ceil(20/5) * 100ms = 400ms`. Peak concurrency never exceeds 5. The key is that `sem <- struct{}{}` happens BEFORE the goroutine launch -- this means the semaphore limits goroutine creation, not just goroutine execution.

## Step 3 -- Semaphore + Context Cancellation

In production, you want both: bounded concurrency AND stop-on-first-error. Combine the semaphore with `context.WithCancel`:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func main() {
	files := generateFileList(20)

	fmt.Println("=== Bounded + Cancellation (limit=5) ===")
	start := time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	validated := 0
	cancelled := 0
	var mu sync.Mutex

	for _, f := range files {
		f := f

		// Check context before acquiring semaphore to avoid blocking
		// on the semaphore when we already know we should stop
		select {
		case <-ctx.Done():
			mu.Lock()
			cancelled++
			mu.Unlock()
			continue
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				mu.Lock()
				cancelled++
				mu.Unlock()
				return
			default:
			}

			if err := validateFile(f); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}

			mu.Lock()
			validated++
			mu.Unlock()
		}()
	}

	wg.Wait()
	fmt.Printf("\nValidated: %d, Cancelled: %d, Total: %d\n", validated, cancelled, len(files))
	fmt.Printf("Total time: %v\n", time.Since(start).Round(time.Millisecond))
	if firstErr != nil {
		fmt.Printf("First error: %v\n", firstErr)
	}
}

func generateFileList(n int) []string {
	files := make([]string, n)
	for i := 0; i < n; i++ {
		files[i] = fmt.Sprintf("config/service-%02d.json", i)
	}
	return files
}

func validateFile(path string) error {
	time.Sleep(100 * time.Millisecond)
	if path == "config/service-07.json" {
		return fmt.Errorf("validate %s: missing required field 'port'", path)
	}
	return nil
}
```

**Expected output:**
```
=== Bounded + Cancellation (limit=5) ===

Validated: 6, Cancelled: 13, Total: 20
Total time: 200ms
First error: validate config/service-07.json: missing required field 'port'
```

The critical addition is checking `ctx.Done()` BEFORE acquiring the semaphore in the for loop. Without this check, the loop would block on `sem <- struct{}{}` even after the context is cancelled. The `select` on `ctx.Done()` allows the loop to skip remaining files immediately.

The `golang.org/x/sync/errgroup` package provides this exact behavior via `SetLimit`:

```go
// With errgroup, the same code becomes:
//   g, ctx := errgroup.WithContext(context.Background())
//   g.SetLimit(5)
//   for _, f := range files {
//       f := f
//       g.Go(func() error {
//           select {
//           case <-ctx.Done():
//               return ctx.Err()
//           default:
//           }
//           return validateFile(f)
//       })
//   }
//   err := g.Wait()
//
// SetLimit(5) replaces the semaphore channel.
// WithContext replaces the manual cancel.
// g.Go() blocks when 5 goroutines are running (backpressure built-in).
```

## Step 4 -- Choosing the Right Concurrency Limit

The limit depends on your bottleneck. Here is a practical guide:

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Println("=== Choosing Concurrency Limits ===")
	fmt.Printf("CPU cores: %d\n\n", runtime.NumCPU())

	limits := []struct {
		scenario string
		limit    int
		reason   string
	}{
		{
			"File validation (disk I/O)",
			10,
			"Limited by file descriptors and disk throughput, not CPU",
		},
		{
			"HTTP health checks",
			20,
			"Limited by connection pool size and target server capacity",
		},
		{
			"CPU-heavy parsing (JSON schema validation)",
			runtime.NumCPU(),
			"Limited by CPU cores -- more goroutines just cause context switching",
		},
		{
			"Database queries",
			5,
			"Limited by connection pool size (typically 5-20 connections)",
		},
		{
			"External API calls (rate-limited)",
			3,
			"Limited by API rate limit (e.g., 100 req/sec = small bursts)",
		},
	}

	for _, l := range limits {
		fmt.Printf("  %-45s limit=%d  (%s)\n", l.scenario, l.limit, l.reason)
	}
}
```

**Expected output:**
```
=== Choosing Concurrency Limits ===
CPU cores: 8

  File validation (disk I/O)                    limit=10   (Limited by file descriptors and disk throughput, not CPU)
  HTTP health checks                            limit=20   (Limited by connection pool size and target server capacity)
  CPU-heavy parsing (JSON schema validation)    limit=8    (Limited by CPU cores -- more goroutines just cause context switching)
  Database queries                              limit=5    (Limited by connection pool size (typically 5-20 connections))
  External API calls (rate-limited)             limit=3    (Limited by API rate limit (e.g., 100 req/sec = small bursts))
```

## Intermediate Verification

At this point, verify:
1. Unbounded concurrency shows all 20 goroutines starting simultaneously
2. Semaphore limits peak concurrency to exactly 5
3. With cancellation, files after the error are skipped
4. Total time follows `ceil(files/limit) * duration_per_file`

## Common Mistakes

### Acquiring the semaphore inside the goroutine

**Wrong:**
```go
wg.Add(1)
go func() {
    defer wg.Done()
    sem <- struct{}{} // acquire INSIDE the goroutine
    defer func() { <-sem }()
    validateFile(f)
}()
```

**What happens:** The goroutine is already launched before acquiring the semaphore. You get unbounded goroutine creation -- 1000 goroutines are created immediately, they just block on the semaphore. Memory usage spikes because each goroutine allocates a stack (at least 2KB, often grows to 8KB+).

**Fix:** Acquire BEFORE launching the goroutine:
```go
sem <- struct{}{} // acquire OUTSIDE -- blocks goroutine creation
wg.Add(1)
go func() {
    defer wg.Done()
    defer func() { <-sem }()
    validateFile(f)
}()
```

### Not checking context before the semaphore acquire

**Wrong:**
```go
for _, f := range files {
    sem <- struct{}{} // blocks even if context is cancelled!
    go func() { ... }()
}
```

**What happens:** After a failure cancels the context, the loop still blocks on `sem <- struct{}{}` waiting for a goroutine to finish and release a token. The loop does not exit until all semaphore slots are freed.

**Fix:** Use `select` to check context first:
```go
select {
case <-ctx.Done():
    continue // skip remaining files
case sem <- struct{}{}:
}
```

### Setting limit to 0

**Wrong:**
```go
sem := make(chan struct{}, 0) // unbuffered -- send blocks until receive
```

**What happens:** `sem <- struct{}{}` blocks until some goroutine does `<-sem`. But no goroutine is running yet. Deadlock on the first iteration.

**Fix:** Always use a positive buffer size.

## Verify What You Learned

Run the full program and confirm:
1. Unbounded concurrency processes all files in ~100ms with 20 concurrent goroutines
2. Semaphore limits peak concurrency to exactly 5 and takes ~400ms
3. Semaphore + cancellation skips files after the first error
4. The concurrency limit guide matches your system's core count

```bash
go run main.go
```

## What's Next
Continue to [04-errgroup-collect-results](../04-errgroup-collect-results/04-errgroup-collect-results.md) to learn how to safely collect results from parallel goroutines into a shared data structure.

## Summary
- Unbounded concurrency can exhaust file descriptors, connections, or memory
- A buffered channel acts as a semaphore: `make(chan struct{}, N)` limits to N concurrent operations
- Acquire the semaphore BEFORE launching the goroutine (not inside it) to limit goroutine creation
- Combine semaphore + `context.WithCancel` for bounded concurrency with fail-fast behavior
- Check `ctx.Done()` before the semaphore acquire to avoid blocking when cancellation is already triggered
- `golang.org/x/sync/errgroup.SetLimit(N)` provides this exact pattern in one line
- Choose the limit based on your bottleneck: file descriptors, connection pools, CPU cores, or API rate limits

## Reference
- [Go Effective Go: Channels as semaphores](https://go.dev/doc/effective_go#channels)
- [errgroup.Group.SetLimit documentation](https://pkg.go.dev/golang.org/x/sync/errgroup#Group.SetLimit)
- [Linux file descriptor limits](https://man7.org/linux/man-pages/man2/getrlimit.2.html)
