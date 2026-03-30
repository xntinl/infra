---
difficulty: advanced
concepts: [semaphore, concurrency-limiting, buffered-channels, resource-management, backpressure]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 9. Buffered Channel as Semaphore

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** a semaphore using a buffered channel to limit concurrency
- **Apply** the acquire/release pattern with `sem <- struct{}{}` and `<-sem`
- **Control** the maximum number of concurrent goroutines accessing a resource
- **Recognize** when to use a semaphore vs. unbounded goroutines

## Why Semaphore With Buffered Channels

Imagine a file downloader that needs to fetch 50 files from a CDN. Launching one goroutine per file is cheap in Go, but opening 50 simultaneous HTTP connections can overwhelm the server, exhaust file descriptors, or trigger rate limiting. You need to cap concurrent downloads to a reasonable number -- say, 3 at a time.

A buffered channel makes a natural semaphore. Create a channel with capacity N -- that is your concurrency limit. Before starting a download, send a value into the channel (acquire a slot). If N goroutines are already downloading, the channel is full and the send blocks -- the goroutine waits its turn. When a download finishes, receive from the channel (release the slot), making room for the next goroutine.

This is lighter than OS semaphores and integrates naturally with Go's channel-based concurrency.

## Step 1 -- The Problem: Unlimited Concurrent Downloads

Launch 10 downloads simultaneously with no limit. All 10 start at the same time, which would overwhelm the target server in production.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    files := []string{
        "report-q1.pdf", "report-q2.pdf", "report-q3.pdf", "report-q4.pdf",
        "data-jan.csv", "data-feb.csv", "data-mar.csv",
        "image-logo.png", "image-banner.png", "image-hero.png",
    }

    var wg sync.WaitGroup
    start := time.Now()

    for _, file := range files {
        wg.Add(1)
        go func(f string) {
            defer wg.Done()
            fmt.Printf("[%s] downloading %s...\n",
                time.Since(start).Round(time.Millisecond), f)
            time.Sleep(300 * time.Millisecond) // simulate download
        }(file)
    }

    wg.Wait()
    fmt.Printf("All done in %v (all 10 ran simultaneously -- too many connections!)\n",
        time.Since(start).Round(time.Millisecond))
}
```

### Verification
```bash
go run main.go
# Expected: all 10 downloads start at +0ms, total ~300ms
# In production, this could exhaust connections or trigger rate limiting
```

## Step 2 -- Add a Semaphore: Limit to 3 Concurrent Downloads

Create a buffered channel of capacity 3. Before downloading, acquire a slot. After finishing, release it. Now only 3 files download at any time.

```go
package main

import (
    "fmt"
    "sync"
    "time"
)

func main() {
    files := []string{
        "report-q1.pdf", "report-q2.pdf", "report-q3.pdf", "report-q4.pdf",
        "data-jan.csv", "data-feb.csv", "data-mar.csv",
        "image-logo.png", "image-banner.png", "image-hero.png",
    }

    // The buffer capacity IS the concurrency limit.
    sem := make(chan struct{}, 3)
    var wg sync.WaitGroup
    start := time.Now()

    for _, file := range files {
        wg.Add(1)
        go func(f string) {
            defer wg.Done()

            // Acquire: blocks when 3 downloads are already active.
            sem <- struct{}{}
            // Release: ALWAYS defer to guarantee the slot is freed, even on panic.
            defer func() { <-sem }()

            elapsed := time.Since(start).Round(time.Millisecond)
            fmt.Printf("[+%6s] downloading %s\n", elapsed, f)
            time.Sleep(300 * time.Millisecond) // simulate download
        }(file)
    }

    wg.Wait()
    fmt.Printf("All done in %v (max 3 concurrent downloads)\n",
        time.Since(start).Round(time.Millisecond))
}
```

Now downloads start in batches of 3. The remaining 7 goroutines wait their turn. Total time is approximately `ceil(10/3) * 300ms = ~1.2s` instead of 300ms, but the server is not overwhelmed.

### Verification
```bash
go run main.go
# Expected: downloads start in batches of ~3, total ~1.2s
```

## Step 3 -- Verify the Concurrency Limit

Track the number of active downloads to prove the semaphore works. This is the kind of verification you would add to an integration test.

```go
package main

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"
)

func main() {
    files := make([]string, 15)
    for i := range files {
        files[i] = fmt.Sprintf("file-%02d.dat", i+1)
    }

    maxConcurrent := 3
    sem := make(chan struct{}, maxConcurrent)
    var wg sync.WaitGroup
    var activeCount atomic.Int32
    var peakConcurrent atomic.Int32
    start := time.Now()

    for _, file := range files {
        wg.Add(1)
        go func(f string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            // Track active downloads.
            current := activeCount.Add(1)
            for {
                old := peakConcurrent.Load()
                if current <= old || peakConcurrent.CompareAndSwap(old, current) {
                    break
                }
            }

            elapsed := time.Since(start).Round(time.Millisecond)
            fmt.Printf("[+%6s] downloading %-15s (active: %d)\n", elapsed, f, current)
            time.Sleep(200 * time.Millisecond)
            activeCount.Add(-1)
        }(file)
    }

    wg.Wait()
    peak := peakConcurrent.Load()
    fmt.Printf("\nPeak concurrent downloads: %d (limit was %d)\n", peak, maxConcurrent)

    if peak <= int32(maxConcurrent) {
        fmt.Println("PASS: concurrency limit respected")
    } else {
        fmt.Println("FAIL: concurrency limit exceeded!")
    }
}
```

### Verification
```bash
go run main.go
# Expected: peak concurrent never exceeds 3, PASS at the end
```

## Step 4 -- Comparing With and Without Semaphore

Side-by-side comparison showing the real consequence of not using a semaphore: all connections open at once versus controlled batching.

```go
package main

import (
    "fmt"
    "sync"
    "sync/atomic"
    "time"
)

func download(files []string, maxParallel int) (time.Duration, int32) {
    sem := make(chan struct{}, maxParallel)
    var wg sync.WaitGroup
    var peak atomic.Int32
    var active atomic.Int32
    start := time.Now()

    for _, f := range files {
        wg.Add(1)
        go func(file string) {
            defer wg.Done()
            if maxParallel > 0 {
                sem <- struct{}{}
                defer func() { <-sem }()
            }
            cur := active.Add(1)
            for {
                old := peak.Load()
                if cur <= old || peak.CompareAndSwap(old, cur) {
                    break
                }
            }
            time.Sleep(200 * time.Millisecond)
            active.Add(-1)
        }(f)
    }

    wg.Wait()
    return time.Since(start).Round(time.Millisecond), peak.Load()
}

func main() {
    files := make([]string, 12)
    for i := range files {
        files[i] = fmt.Sprintf("file-%02d.dat", i+1)
    }

    elapsed, peak := download(files, 12) // no effective limit
    fmt.Printf("Unlimited:  %v, peak connections: %d (server overloaded!)\n", elapsed, peak)

    elapsed, peak = download(files, 3) // semaphore at 3
    fmt.Printf("Limited(3): %v, peak connections: %d (safe)\n", elapsed, peak)
}
```

### Verification
```bash
go run main.go
# Expected:
#   Unlimited:  ~200ms, peak connections: 12 (server overloaded!)
#   Limited(3): ~800ms, peak connections: 3 (safe)
```

## Step 5 -- Downloader with Progress Reporting

A production-ready downloader that limits concurrency and reports progress. This combines the semaphore with channel-based result collection.

```go
package main

import (
    "fmt"
    "math/rand"
    "sync"
    "time"
)

type DownloadResult struct {
    File     string
    Size     int
    Duration time.Duration
    Error    string
}

func main() {
    files := []string{
        "report-2024-q1.pdf", "report-2024-q2.pdf", "report-2024-q3.pdf",
        "dataset-users.csv", "dataset-orders.csv",
        "backup-db-full.tar.gz", "backup-db-incremental.tar.gz",
        "config-prod.yaml", "config-staging.yaml",
    }

    sem := make(chan struct{}, 3)
    results := make(chan DownloadResult, len(files))
    var wg sync.WaitGroup
    start := time.Now()

    for _, file := range files {
        wg.Add(1)
        go func(f string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            dlStart := time.Now()
            duration := time.Duration(100+rand.Intn(300)) * time.Millisecond
            time.Sleep(duration) // simulate variable download time

            size := 1024 + rand.Intn(10240) // simulate file size in KB
            results <- DownloadResult{
                File:     f,
                Size:     size,
                Duration: time.Since(dlStart).Round(time.Millisecond),
            }
        }(file)
    }

    // Close results channel after all downloads complete.
    go func() {
        wg.Wait()
        close(results)
    }()

    // Collect and display results as they arrive.
    totalSize := 0
    downloaded := 0
    for res := range results {
        downloaded++
        totalSize += res.Size
        fmt.Printf("  [%d/%d] %-35s %5d KB in %v\n",
            downloaded, len(files), res.File, res.Size, res.Duration)
    }

    fmt.Printf("\nCompleted: %d files, %d KB total, %v elapsed (max 3 concurrent)\n",
        downloaded, totalSize, time.Since(start).Round(time.Millisecond))
}
```

### Verification
```bash
go run main.go
# Expected: files download in batches of 3, with progress reported as each completes
```

## Intermediate Verification

Run the programs and confirm:
1. Without the semaphore, all goroutines start simultaneously
2. With the semaphore, peak concurrency never exceeds the limit
3. Total time increases proportionally to the number of batches
4. The `defer` release ensures slots are freed even if work panics

## Common Mistakes

### Forgetting to Release the Semaphore

**Wrong:**
```go
sem <- struct{}{}
doWork() // if this panics, the slot is never released!
<-sem    // never reached
```

**What happens:** One slot is permanently consumed. Eventually all slots are stuck and no new work can start. The program hangs.

**Fix:** Always use `defer` immediately after acquiring:
```go
sem <- struct{}{}
defer func() { <-sem }()
doWork() // even if this panics, defer releases the slot
```

### Using the Semaphore Backwards

**Wrong:**
```go
sem := make(chan struct{}, 3)
<-sem        // receive first -- blocks forever on empty channel!
defer func() { sem <- struct{}{} }()
```

**What happens:** The "acquire" receive blocks because the channel starts empty. No goroutine ever proceeds.

**Fix:** Send to acquire (fills buffer), receive to release (drains buffer):
```go
sem <- struct{}{}        // acquire: fill a slot
defer func() { <-sem }() // release: drain a slot
```

## Verify What You Learned
1. Why does the buffer capacity of the semaphore channel equal the concurrency limit?
2. What happens if you forget `defer` on the release and the download panics?
3. When would you choose a semaphore over just limiting the number of goroutines you launch?

## What's Next
Continue to [10-channel-vs-shared-memory](../10-channel-vs-shared-memory/10-channel-vs-shared-memory.md) to compare channel-based and mutex-based approaches to the same problem.

## Summary
- `sem := make(chan struct{}, N)` creates a semaphore with capacity N
- `sem <- struct{}{}` acquires a slot (blocks if N slots are taken)
- `<-sem` releases a slot (always `defer` this to prevent leaks)
- The buffer capacity equals the maximum concurrent operations
- Use `defer func() { <-sem }()` immediately after acquiring for panic safety
- Semaphores prevent resource exhaustion (connections, file handles, API rate limits)

## Reference
- [Effective Go: Channels as semaphores](https://go.dev/doc/effective_go#channels)
- [golang.org/x/sync/semaphore](https://pkg.go.dev/golang.org/x/sync/semaphore) (standard library weighted semaphore)
- [Go Blog: Bounded concurrency](https://go.dev/blog/pipelines)
