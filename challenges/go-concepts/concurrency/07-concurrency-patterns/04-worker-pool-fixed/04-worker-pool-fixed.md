---
difficulty: intermediate
concepts: [worker pool, job queue, result collection, goroutine lifecycle]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [goroutines, channels, sync.WaitGroup, fan-out, fan-in]
---

# 4. Worker Pool (Fixed)

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a fixed-size worker pool with typed Job and Result structs
- **Separate** job submission from result collection using distinct channels
- **Manage** worker lifecycle with proper channel closing and WaitGroup
- **Measure** throughput with different pool sizes and observe backpressure

## Why Worker Pools

The worker pool is the most widely used concurrency pattern in Go. It combines fan-out and fan-in into a single, structured unit: a fixed number of goroutines (workers) pull jobs from a shared queue, process them, and push results into a collection channel.

Consider a real scenario: your service handles user-uploaded images that need thumbnail generation. Each resize operation takes 200-500ms of CPU time. Without a pool, 1000 concurrent uploads would spawn 1000 goroutines all competing for CPU cores, causing context switching overhead and memory pressure. A fixed pool of N workers (where N matches your CPU count) caps resource usage while keeping all cores busy. When all workers are occupied, new submissions block -- this is backpressure, preventing your system from accepting more work than it can handle.

```
  Image Thumbnail Worker Pool

  +--------+    +------+    +---------+
  |uploads | -> | jobs | -> | worker1 | --+
  +--------+    | chan  | -> | worker2 | --+--> results chan --> storage
                |      | -> | worker3 | --+
                |      | -> | worker4 | --+
                +------+    +---------+

  Flow: submit jobs -> close jobs -> workers drain queue
  -> workers exit -> WaitGroup zero -> close results
  -> collector finishes
```

## Step 1 -- Define Job and Result Types

Start by defining structured types for the image resize jobs and results. This makes the pool type-safe and traceable.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ImageJob struct {
	ID       int
	Filename string
	Width    int
	Height   int
}

type ThumbnailResult struct {
	Job      ImageJob
	Output   string
	Duration time.Duration
	WorkerID int
	Error    error
}

func thumbnailWorker(id int, jobs <-chan ImageJob, results chan<- ThumbnailResult) {
	for job := range jobs {
		start := time.Now()

		// Simulate CPU-intensive resize (varies by image size)
		workTime := time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(workTime)

		output := fmt.Sprintf("thumb_%dx%d_%s", job.Width/4, job.Height/4, job.Filename)

		results <- ThumbnailResult{
			Job:      job,
			Output:   output,
			Duration: time.Since(start),
			WorkerID: id,
		}
	}
}

func main() {
	jobs := make(chan ImageJob, 3)
	results := make(chan ThumbnailResult, 3)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		thumbnailWorker(1, jobs, results)
	}()

	// Submit 3 jobs to a single worker
	images := []ImageJob{
		{ID: 1, Filename: "vacation.jpg", Width: 4000, Height: 3000},
		{ID: 2, Filename: "profile.png", Width: 2000, Height: 2000},
		{ID: 3, Filename: "banner.jpg", Width: 6000, Height: 2000},
	}

	for _, img := range images {
		jobs <- img
	}
	close(jobs)

	wg.Wait()
	close(results)

	fmt.Println("=== Single Worker Test ===")
	for r := range results {
		fmt.Printf("  %s -> %s (%v)\n", r.Job.Filename, r.Output, r.Duration)
	}
}
```

Each `ThumbnailResult` carries the original job, the output filename, timing, and which worker processed it. This traceability is invaluable for debugging and monitoring in production.

### Intermediate Verification
```bash
go run main.go
```
A single worker processes all jobs sequentially:
```
=== Single Worker Test ===
  vacation.jpg -> thumb_1000x750_vacation.jpg (87ms)
  profile.png -> thumb_500x500_profile.png (112ms)
  banner.jpg -> thumb_1500x500_banner.jpg (65ms)
```

## Step 2 -- Build the Pool

Now create the full pool: launch N workers, send jobs, and collect results. This is where the concurrency benefit becomes visible.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ImageJob struct {
	ID       int
	Filename string
	Width    int
	Height   int
}

type ThumbnailResult struct {
	Job      ImageJob
	Output   string
	Duration time.Duration
	WorkerID int
}

func thumbnailWorker(id int, jobs <-chan ImageJob, results chan<- ThumbnailResult) {
	for job := range jobs {
		start := time.Now()
		workTime := time.Duration(50+rand.Intn(100)) * time.Millisecond
		time.Sleep(workTime)
		output := fmt.Sprintf("thumb_%dx%d_%s", job.Width/4, job.Height/4, job.Filename)
		results <- ThumbnailResult{
			Job: job, Output: output,
			Duration: time.Since(start), WorkerID: id,
		}
	}
}

func runPool(numWorkers int, images []ImageJob) {
	jobs := make(chan ImageJob, len(images))
	results := make(chan ThumbnailResult, len(images))

	var wg sync.WaitGroup
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			thumbnailWorker(id, jobs, results)
		}(w)
	}

	for _, img := range images {
		jobs <- img
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		fmt.Printf("  [worker %d] %s -> %s (%v)\n",
			r.WorkerID, r.Job.Filename, r.Output, r.Duration)
	}
}

func main() {
	images := make([]ImageJob, 12)
	for i := range images {
		images[i] = ImageJob{
			ID:       i + 1,
			Filename: fmt.Sprintf("photo_%02d.jpg", i+1),
			Width:    2000 + rand.Intn(4000),
			Height:   1500 + rand.Intn(3000),
		}
	}

	fmt.Println("=== Thumbnail Pool (4 workers, 12 images) ===")
	start := time.Now()
	runPool(4, images)
	fmt.Printf("  Completed in %v\n", time.Since(start))
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: all 12 images processed, distributed across 4 workers:
```
=== Thumbnail Pool (4 workers, 12 images) ===
  [worker 2] photo_01.jpg -> thumb_750x500_photo_01.jpg (67ms)
  [worker 4] photo_03.jpg -> thumb_1250x875_photo_03.jpg (95ms)
  ...
  Completed in 310ms
```

## Step 3 -- Measure Pool Performance

Compare execution time with different pool sizes to see the concurrency benefit and observe diminishing returns.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ImageJob struct {
	ID       int
	Filename string
	Width    int
	Height   int
}

type ThumbnailResult struct {
	Job      ImageJob
	Duration time.Duration
	WorkerID int
}

func thumbnailWorker(id int, jobs <-chan ImageJob, results chan<- ThumbnailResult) {
	for job := range jobs {
		start := time.Now()
		time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
		results <- ThumbnailResult{Job: job, Duration: time.Since(start), WorkerID: id}
	}
}

func main() {
	numImages := 24

	fmt.Println("=== Pool Size vs Throughput ===")
	fmt.Printf("  Processing %d images\n\n", numImages)

	for _, poolSize := range []int{1, 2, 4, 8, 12, 24} {
		images := make([]ImageJob, numImages)
		for i := range images {
			images[i] = ImageJob{
				ID: i + 1, Filename: fmt.Sprintf("img_%02d.jpg", i+1),
				Width: 3000, Height: 2000,
			}
		}

		start := time.Now()
		jobs := make(chan ImageJob, numImages)
		results := make(chan ThumbnailResult, numImages)

		var wg sync.WaitGroup
		for w := 0; w < poolSize; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				thumbnailWorker(id, jobs, results)
			}(w)
		}

		for _, img := range images {
			jobs <- img
		}
		close(jobs)

		go func() {
			wg.Wait()
			close(results)
		}()

		var totalWork time.Duration
		workerCounts := make(map[int]int)
		for r := range results {
			totalWork += r.Duration
			workerCounts[r.WorkerID]++
		}

		elapsed := time.Since(start)
		fmt.Printf("  %2d workers: %v (total CPU work: %v, efficiency: %.0f%%)\n",
			poolSize, elapsed, totalWork, float64(totalWork)/float64(elapsed*time.Duration(poolSize))*100)
	}
}
```

### Intermediate Verification
```bash
go run main.go
```
More workers reduce wall-clock time, but notice diminishing returns:
```
=== Pool Size vs Throughput ===
  Processing 24 images

   1 workers: 2.1s  (total CPU work: 2.1s, efficiency: 100%)
   2 workers: 1.05s (total CPU work: 2.1s, efficiency: 100%)
   4 workers: 540ms (total CPU work: 2.1s, efficiency: 97%)
   8 workers: 280ms (total CPU work: 2.1s, efficiency: 94%)
  12 workers: 195ms (total CPU work: 2.1s, efficiency: 89%)
  24 workers: 120ms (total CPU work: 2.1s, efficiency: 72%)
```

## Step 4 -- Observe Backpressure

Demonstrate what happens when the job queue is small and all workers are busy: the producer blocks, providing natural backpressure.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type ImageJob struct {
	ID       int
	Filename string
}

func thumbnailWorker(id int, jobs <-chan ImageJob, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		fmt.Printf("  [worker %d] processing %s\n", id, job.Filename)
		time.Sleep(time.Duration(100+rand.Intn(100)) * time.Millisecond)
		fmt.Printf("  [worker %d] finished  %s\n", id, job.Filename)
	}
}

func main() {
	fmt.Println("=== Backpressure Demo (2 workers, buffer=1) ===")
	fmt.Println("  Producer blocks when both workers are busy and buffer is full")
	fmt.Println()

	jobs := make(chan ImageJob, 1) // tiny buffer
	var wg sync.WaitGroup

	for w := 1; w <= 2; w++ {
		wg.Add(1)
		go thumbnailWorker(w, jobs, &wg)
	}

	for i := 1; i <= 8; i++ {
		job := ImageJob{ID: i, Filename: fmt.Sprintf("upload_%02d.jpg", i)}
		fmt.Printf("  [producer] submitting %s...\n", job.Filename)
		submitStart := time.Now()
		jobs <- job
		if wait := time.Since(submitStart); wait > 5*time.Millisecond {
			fmt.Printf("  [producer] blocked for %v (backpressure!)\n", wait)
		}
	}
	close(jobs)
	wg.Wait()
	fmt.Println("\n  All images processed")
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: you see the producer blocking when workers are busy:
```
=== Backpressure Demo (2 workers, buffer=1) ===
  Producer blocks when both workers are busy and buffer is full

  [producer] submitting upload_01.jpg...
  [producer] submitting upload_02.jpg...
  [producer] submitting upload_03.jpg...
  [worker 1] processing upload_01.jpg
  [worker 2] processing upload_02.jpg
  [producer] submitting upload_04.jpg...
  [producer] blocked for 105ms (backpressure!)
  ...
```

## Common Mistakes

### Sending Results After the Channel is Closed
**Wrong:**
```go
go func() {
	wg.Wait()
	close(results)
}()
// Worker still running, sends to closed results -> panic
```
**What happens:** If the WaitGroup is not properly coordinated, a worker might try to send after results is closed.

**Fix:** Ensure every worker calls `wg.Done()` only after it has finished all sends. The `defer wg.Done()` at the top of the worker goroutine guarantees this, since `range jobs` exits only when jobs is closed and drained.

### Forgetting to Buffer the Channels
**Wrong:**
```go
jobs := make(chan ImageJob)       // unbuffered
results := make(chan ThumbnailResult) // unbuffered
```
**What happens:** With unbuffered channels, the sender blocks until a receiver is ready. If you try to send all jobs before collecting results, you deadlock (job send blocks because no worker can receive because it's blocked trying to send a result).

**Fix:** Buffer at least one of the channels, or send jobs and collect results concurrently.

### Pool Size of Zero
Always validate that the number of workers is at least 1. A pool with zero workers means nobody reads from the jobs channel, causing a deadlock.

## Verify What You Learned

Run `go run main.go` and verify:
- Single worker: 3 images processed sequentially
- Pool with 4 workers and 12 images: all results collected, roughly 3x faster than 1 worker
- Performance benchmark: more workers = faster (up to a point), then diminishing returns
- Backpressure demo: producer blocks when all workers are busy and buffer is full

## What's Next
Continue to [05-semaphore-bounded-concurrency](../05-semaphore-bounded-concurrency/05-semaphore-bounded-concurrency.md) to learn an alternative approach to bounding concurrency using a buffered channel as a semaphore.

## Summary
- A worker pool is a fixed set of goroutines reading from a shared jobs channel
- Separate channels for jobs (input) and results (output) provide clean separation
- Typed Job and Result structs make the pool type-safe and debuggable
- Close the jobs channel to signal workers to stop, use WaitGroup to close results
- Buffer channels to avoid deadlocks when sending and receiving happen in sequence
- Worker pools provide bounded concurrency and natural backpressure
- Match pool size to your bottleneck: CPU cores for CPU-bound work, connection limits for I/O-bound work

## Reference
- [Go by Example: Worker Pools](https://gobyexample.com/worker-pools)
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Effective Go: Parallelization](https://go.dev/doc/effective_go#parallel)
