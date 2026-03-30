---
difficulty: basic
concepts: [context.WithCancel, cancel function, ctx.Done channel, cancellation propagation, goroutine leaks]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 2. Context WithCancel

## Learning Objectives
After completing this exercise, you will be able to:
- **Create** a cancellable context using `context.WithCancel`
- **Propagate** cancellation to multiple concurrent data source queries
- **Detect** goroutine leaks caused by missing cancellation
- **Implement** a "first result wins, cancel the rest" pattern

## Why WithCancel

In real services, a single user action often triggers multiple concurrent operations. A search might query a database, a cache, and a full-text index simultaneously. When the user cancels the search -- or when one source returns a result -- all other queries should stop immediately. Without cancellation, those goroutines keep running, holding connections, consuming CPU, and leaking memory.

`context.WithCancel` creates a derived context paired with a `cancel` function. When you call `cancel()`, the context's `Done()` channel closes, and every goroutine listening on that channel receives the signal simultaneously. This is cooperative cancellation: the goroutine must explicitly check `ctx.Done()` and choose to stop. The context does not forcibly kill anything -- it sends a signal that the goroutine must honor.

The real consequence of not using cancellation: in a service handling 10,000 requests per second, each leaking one goroutine, you will exhaust memory in minutes. This is not hypothetical -- it is one of the most common production incidents in Go services.

## Step 1 -- Multi-Source User Search with Cancellation

Build a user search that queries three data sources concurrently. When the user clicks "cancel" (simulated), all ongoing queries stop:

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

type SearchAggregator struct{}

func NewSearchAggregator() *SearchAggregator {
	return &SearchAggregator{}
}

func (s *SearchAggregator) searchDatabase(ctx context.Context, query string, results chan<- string) {
	delay := time.Duration(100+rand.Intn(200)) * time.Millisecond
	select {
	case <-time.After(delay):
		results <- fmt.Sprintf("database: found user %q in %v", query, delay)
	case <-ctx.Done():
		fmt.Printf("[database]    search cancelled: %v\n", ctx.Err())
	}
}

func (s *SearchAggregator) searchCache(ctx context.Context, query string, results chan<- string) {
	delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
	select {
	case <-time.After(delay):
		results <- fmt.Sprintf("cache: found user %q in %v", query, delay)
	case <-ctx.Done():
		fmt.Printf("[cache]       search cancelled: %v\n", ctx.Err())
	}
}

func (s *SearchAggregator) searchIndex(ctx context.Context, query string, results chan<- string) {
	delay := time.Duration(150+rand.Intn(300)) * time.Millisecond
	select {
	case <-time.After(delay):
		results <- fmt.Sprintf("index: found user %q in %v", query, delay)
	case <-ctx.Done():
		fmt.Printf("[full-index]  search cancelled: %v\n", ctx.Err())
	}
}

func (s *SearchAggregator) SearchAll(ctx context.Context, query string) <-chan string {
	results := make(chan string, 3)
	go s.searchDatabase(ctx, query, results)
	go s.searchCache(ctx, query, results)
	go s.searchIndex(ctx, query, results)
	return results
}

func simulateUserCancel(cancel context.CancelFunc, after time.Duration) {
	time.Sleep(after)
	fmt.Println("\n[user] clicked cancel")
	cancel()
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	aggregator := NewSearchAggregator()

	fmt.Println("Starting user search across 3 sources...")
	results := aggregator.SearchAll(ctx, "alice@example.com")

	go simulateUserCancel(cancel, 120*time.Millisecond)

	for {
		select {
		case result := <-results:
			fmt.Printf("[result] %s\n", result)
		case <-ctx.Done():
			fmt.Printf("\nSearch ended: %v\n", ctx.Err())
			time.Sleep(50 * time.Millisecond)
			return
		}
	}
}
```

### Verification
```bash
go run main.go
```
Expected output (timing varies, some sources may return before cancel):
```
Starting user search across 3 sources...
[result] cache: found user "alice@example.com" in 73ms

[user] clicked cancel
[database]    search cancelled: context canceled
[full-index]  search cancelled: context canceled

Search ended: context canceled
```

When cancel is called, all goroutines listening on `ctx.Done()` receive the signal. Sources that finished before the cancel return results; sources still running get cancelled. No goroutine is left behind.

## Step 2 -- Goroutine Leak: What Happens Without Cancellation

This is the critical anti-pattern. When you launch goroutines without a cancellable context, they keep running even after nobody cares about their results. Run this example and observe the leak:

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

type LeakySearcher struct{}

func (l *LeakySearcher) search(query string, results chan<- string) {
	time.Sleep(500 * time.Millisecond)
	select {
	case results <- fmt.Sprintf("found: %s", query):
	default:
	}
}

func (l *LeakySearcher) SearchWithoutContext(query string) string {
	results := make(chan string, 3)

	go l.search(query+"-db", results)
	go l.search(query+"-cache", results)
	go l.search(query+"-index", results)

	return <-results
}

func reportGoroutines(label string) int {
	count := runtime.NumGoroutine()
	fmt.Printf("%s: %d\n", label, count)
	return count
}

func main() {
	searcher := &LeakySearcher{}

	before := reportGoroutines("Goroutines before")

	for i := 0; i < 5; i++ {
		result := searcher.SearchWithoutContext(fmt.Sprintf("query-%d", i))
		fmt.Printf("Request %d: %s\n", i, result)
	}

	after := reportGoroutines("\nGoroutines after")
	fmt.Printf("Leaked goroutines: %d\n", after-before)
	fmt.Println("Each request leaks ~2 goroutines (the 2 slower sources).")
	fmt.Println("At 10,000 req/s, this exhausts memory in minutes.")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Goroutines before: 1
Request 0: found: query-0-db
Request 1: found: query-1-db
Request 2: found: query-2-db
Request 3: found: query-3-db
Request 4: found: query-4-db

Goroutines after: 11
Leaked goroutines: 10
Each request leaks ~2 goroutines (the 2 slower sources).
At 10,000 req/s, this exhausts memory in minutes.
```

Each request launches 3 goroutines but only consumes 1 result. The other 2 goroutines have no way to know they should stop. This is the most common goroutine leak in production Go code.

## Step 3 -- First Result Wins with Proper Cancellation

Fix the leak from Step 2. Use `WithCancel` to stop all remaining queries as soon as the first result arrives:

```go
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

type DataSource struct {
	Name  string
	Delay time.Duration
}

type SafeSearcher struct {
	sources []DataSource
}

func NewSafeSearcher() *SafeSearcher {
	return &SafeSearcher{
		sources: []DataSource{
			{Name: "database", Delay: 200 * time.Millisecond},
			{Name: "cache", Delay: 80 * time.Millisecond},
			{Name: "index", Delay: 350 * time.Millisecond},
		},
	}
}

func (s *SafeSearcher) querySource(ctx context.Context, source DataSource, results chan<- string) {
	select {
	case <-time.After(source.Delay):
		select {
		case results <- fmt.Sprintf("[%s] found result in %v", source.Name, source.Delay):
		case <-ctx.Done():
		}
	case <-ctx.Done():
		fmt.Printf("  [%s] cancelled, releasing resources\n", source.Name)
	}
}

func (s *SafeSearcher) FirstResult(query string) string {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan string, len(s.sources))
	for _, src := range s.sources {
		go s.querySource(ctx, src, results)
	}

	return <-results
}

func main() {
	searcher := NewSafeSearcher()

	goroutinesBefore := runtime.NumGoroutine()
	fmt.Printf("Goroutines before: %d\n\n", goroutinesBefore)

	for i := 0; i < 5; i++ {
		result := searcher.FirstResult(fmt.Sprintf("query-%d", i))
		fmt.Printf("Request %d: %s\n", i, result)
		time.Sleep(100 * time.Millisecond)
		fmt.Println()
	}

	time.Sleep(200 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	fmt.Printf("Goroutines after:  %d\n", goroutinesAfter)
	fmt.Printf("Leaked goroutines: %d (should be 0)\n", goroutinesAfter-goroutinesBefore)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Goroutines before: 1

Request 0: [cache] found result in 80ms
  [database] cancelled, releasing resources
  [index] cancelled, releasing resources

Request 1: [cache] found result in 80ms
  [database] cancelled, releasing resources
  [index] cancelled, releasing resources

...

Goroutines after:  1
Leaked goroutines: 0 (should be 0)
```

When `defer cancel()` runs on return, it closes the `Done()` channel, and the remaining goroutines detect this and exit. Zero goroutine leaks, no matter how many requests you handle.

## Step 4 -- Cancellation Propagates Down, Never Up

In a real system, you might cancel a sub-operation without affecting the parent. Cancelling a child context leaves the parent and siblings unaffected:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

const replicaQueryDelay = 200 * time.Millisecond

type ReplicaQueryResult struct {
	Name    string
	Message string
}

type DatabaseCluster struct{}

func NewDatabaseCluster() *DatabaseCluster {
	return &DatabaseCluster{}
}

func (d *DatabaseCluster) QueryReplica(ctx context.Context, name string, done chan<- ReplicaQueryResult) {
	select {
	case <-time.After(replicaQueryDelay):
		done <- ReplicaQueryResult{Name: name, Message: "query complete"}
	case <-ctx.Done():
		done <- ReplicaQueryResult{Name: name, Message: fmt.Sprintf("cancelled (%v)", ctx.Err())}
	}
}

func (d *DatabaseCluster) DemonstrateCancellationScope() {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	primaryCtx, cancelPrimary := context.WithCancel(parent)
	replicaCtx, cancelReplica := context.WithCancel(parent)
	defer cancelReplica()

	primaryDone := make(chan ReplicaQueryResult, 1)
	replicaDone := make(chan ReplicaQueryResult, 1)

	go d.QueryReplica(primaryCtx, "primary-db", primaryDone)
	go d.QueryReplica(replicaCtx, "replica-db", replicaDone)

	fmt.Println("Cancelling primary query only...")
	cancelPrimary()

	primary := <-primaryDone
	replica := <-replicaDone
	fmt.Printf("%s: %s\n", primary.Name, primary.Message)
	fmt.Printf("%s: %s\n", replica.Name, replica.Message)

	fmt.Printf("\nparent.Err():  %v (unaffected)\n", parent.Err())
	fmt.Printf("primary.Err(): %v (cancelled)\n", primaryCtx.Err())
	fmt.Printf("replica.Err(): %v (still running)\n", replicaCtx.Err())
}

func main() {
	cluster := NewDatabaseCluster()
	cluster.DemonstrateCancellationScope()
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
Cancelling primary query only...
primary-db: cancelled (context canceled)
replica-db: query complete

parent.Err():  <nil> (unaffected)
primary.Err(): context canceled (cancelled)
replica.Err(): <nil> (still running)
```

Cancellation flows down, never up. This is critical: a failing sub-operation should not tear down unrelated parts of the system. The replica query continues undisturbed.

## Common Mistakes

### Forgetting to Call Cancel (Goroutine Leak)
**Wrong:**
```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // unused -- resource leak!
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```
**What happens:** The derived context and its internal goroutine are never cleaned up. The Go runtime cannot garbage-collect the context's internal resources until cancel is called.

**Fix:** Always `defer cancel()` immediately after creating the context:
```go
package main

import (
	"context"
	"fmt"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fmt.Printf("ctx.Err(): %v\n", ctx.Err())
}
```

### Not Checking ctx.Done() in the Goroutine
**Wrong:**
```go
func processQueue(ctx context.Context, items []string) {
    for _, item := range items {
        heavyProcessing(item) // never checks ctx.Done() -- runs forever
    }
}
```
**What happens:** The goroutine ignores the cancellation signal and continues consuming CPU and memory. Calling `cancel()` has no effect because nobody is listening.

**Fix:** Check cancellation between units of work:
```go
func processQueue(ctx context.Context, items []string) {
    for _, item := range items {
        select {
        case <-ctx.Done():
            return // stop processing
        default:
        }
        heavyProcessing(item)
    }
}
```

### Passing the Cancel Function to Other Goroutines
Prefer keeping the cancel function close to where the context was created. Passing it to multiple goroutines makes it unclear who is responsible for cancellation, leading to premature or accidental cancellation. If a goroutine needs to signal that an operation should stop, use a separate channel and let the owner call cancel.

## Verify What You Learned

Build a concurrent file search that checks three directories. Use `WithCancel` so that when the first directory finds the file, the other searches stop. Verify zero goroutine leaks:

```go
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

type DirectorySearch struct {
	Path  string
	Delay time.Duration
}

type FileSearcher struct {
	directories []DirectorySearch
}

func NewFileSearcher() *FileSearcher {
	return &FileSearcher{
		directories: []DirectorySearch{
			{Path: "/var/log", Delay: 300 * time.Millisecond},
			{Path: "/tmp", Delay: 100 * time.Millisecond},
			{Path: "/home", Delay: 500 * time.Millisecond},
		},
	}
}

func (f *FileSearcher) searchDirectory(ctx context.Context, dir DirectorySearch, results chan<- string) {
	select {
	case <-time.After(dir.Delay):
		select {
		case results <- fmt.Sprintf("found in %s (took %v)", dir.Path, dir.Delay):
		case <-ctx.Done():
		}
	case <-ctx.Done():
		fmt.Printf("  [%s] search cancelled\n", dir.Path)
	}
}

func (f *FileSearcher) FindFirst(ctx context.Context) string {
	results := make(chan string, len(f.directories))
	for _, dir := range f.directories {
		go f.searchDirectory(ctx, dir, results)
	}
	return <-results
}

func main() {
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	searcher := NewFileSearcher()
	first := searcher.FindFirst(ctx)
	fmt.Printf("First result: %s\n", first)
	cancel()

	time.Sleep(100 * time.Millisecond)

	after := runtime.NumGoroutine()
	fmt.Printf("\nGoroutines before: %d, after: %d, leaked: %d\n", before, after, after-before)
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
First result: found in /tmp (took 100ms)
  [/var/log] search cancelled
  [/home] search cancelled

Goroutines before: 1, after: 1, leaked: 0
```

## What's Next
Continue to [03-context-withtimeout](../03-context-withtimeout/03-context-withtimeout.md) to learn how to automatically cancel operations that take too long, protecting your service from slow dependencies.

## Summary
- `context.WithCancel` returns a derived context and a `cancel` function
- Calling `cancel()` closes the `Done()` channel, signaling all listeners simultaneously
- The "first result wins" pattern: launch concurrent queries, take the first result, cancel the rest
- Without cancellation, goroutines leak -- each leaked goroutine holds memory and potentially a connection
- Cancellation propagates from parent to all descendants but never upward to parent or siblings
- Always `defer cancel()` to prevent resource leaks
- Calling cancel multiple times is safe (idempotent)
- Goroutines must cooperatively check `ctx.Done()` to respond to cancellation

## Reference
- [Package context: WithCancel](https://pkg.go.dev/context#WithCancel)
- [Go Blog: Context](https://go.dev/blog/context)
- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
