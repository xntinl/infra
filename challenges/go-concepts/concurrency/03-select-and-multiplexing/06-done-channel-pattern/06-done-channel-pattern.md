---
difficulty: intermediate
concepts: [done-channel, cancellation, close-broadcast, goroutine-lifecycle, context-foundation]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 6. Done Channel Pattern

## Learning Objectives
- **Implement** a done channel to signal cancellation to one or more goroutines
- **Explain** why closing a channel is a broadcast mechanism
- **Propagate** cancellation across a multi-stage pipeline

## Why Done Channels

Goroutines are not preemptible in the traditional OS sense. You cannot kill a goroutine from outside. The only way to stop a goroutine is to make it stop itself by giving it a signal it checks voluntarily. The done channel is that signal.

Consider a background worker that periodically refreshes a cache -- fetching data from a database every 30 seconds and updating an in-memory cache. When the application shuts down, this worker must stop cleanly: finish its current refresh, close database connections, and flush any pending writes. If you just exit `main`, the goroutine is killed mid-operation, potentially leaving corrupted state.

When you close a channel, every receiver waiting on it unblocks immediately. This makes close a broadcast operation: one close wakes up an unlimited number of listeners. A done channel exploits this property. You create a `chan struct{}` (zero-size, carries no data), pass it to all goroutines, and close it when you want them to stop. Every goroutine that checks this channel in its `select` will see the close and can exit cleanly.

This pattern is so fundamental that it was formalized into `context.Context` in Go 1.7. The `ctx.Done()` method returns exactly this kind of channel. Understanding the raw done channel pattern gives you deep intuition for how context cancellation works under the hood.

## Step 1 -- Stop a Background Cache Refresher

Create a background worker that periodically refreshes a cache. Main signals cancellation by closing a done channel. The worker stops cleanly.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	refreshed := make(chan string)

	// Background cache refresher.
	go func() {
		defer close(refreshed)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		cycle := 0
		for {
			select {
			case <-done:
				fmt.Println("refresher: received cancellation, stopping")
				return
			case <-ticker.C:
				cycle++
				result := fmt.Sprintf("cache refreshed (cycle %d)", cycle)
				select {
				case <-done:
					return
				case refreshed <- result:
				}
			}
		}
	}()

	// Consume 5 refresh cycles, then cancel.
	for i := 0; i < 5; i++ {
		fmt.Println("main:", <-refreshed)
	}

	close(done)
	time.Sleep(50 * time.Millisecond)
	fmt.Println("main: cache refresher stopped")
}
```

The worker produces refresh results until the done channel is closed. The main goroutine consumes 5 cycles, then signals cancellation.

### Verification
```
main: cache refreshed (cycle 1)
main: cache refreshed (cycle 2)
main: cache refreshed (cycle 3)
main: cache refreshed (cycle 4)
main: cache refreshed (cycle 5)
refresher: received cancellation, stopping
main: cache refresher stopped
```

## Step 2 -- Broadcasting Cancellation to Multiple Workers

Close one channel to stop multiple cache refreshers simultaneously. This is how a microservice shuts down its background workers on SIGTERM.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func cacheRefresher(id int, done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			fmt.Printf("worker-%d: flushing pending writes and stopping\n", id)
			return
		case <-ticker.C:
			fmt.Printf("worker-%d: refreshed cache partition\n", id)
		}
	}
}

func main() {
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go cacheRefresher(i, done, &wg)
	}

	time.Sleep(350 * time.Millisecond)
	fmt.Println("main: shutting down all workers")
	close(done) // One close stops all three.
	wg.Wait()
	fmt.Println("main: all workers stopped cleanly")
}
```

One `close(done)` stops all three workers. You do not need to track or signal each goroutine individually.

### Verification
```
worker-1: refreshed cache partition
worker-2: refreshed cache partition
worker-3: refreshed cache partition
...
main: shutting down all workers
worker-2: flushing pending writes and stopping
worker-1: flushing pending writes and stopping
worker-3: flushing pending writes and stopping
main: all workers stopped cleanly
```

## Step 3 -- Pipeline Cancellation (Fetch -> Transform -> Store)

Build a two-stage data pipeline where cancellation flows from the consumer through all stages. Stage 1 fetches data, Stage 2 transforms it. Both must stop cleanly when the consumer has enough.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	done := make(chan struct{})
	var wg sync.WaitGroup

	// Stage 1: Fetches records from "database".
	fetchOut := make(chan string)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(fetchOut)
		i := 0
		for {
			record := fmt.Sprintf("record-%d", i)
			select {
			case <-done:
				fmt.Println("fetcher: cancelled, closing connection")
				return
			case fetchOut <- record:
				i++
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Stage 2: Transforms records (e.g., enrichment, formatting).
	transformOut := make(chan string)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(transformOut)
		for {
			select {
			case <-done:
				fmt.Println("transformer: cancelled, flushing buffer")
				return
			case record, ok := <-fetchOut:
				if !ok {
					return
				}
				transformed := fmt.Sprintf("transformed(%s)", record)
				select {
				case <-done:
					return
				case transformOut <- transformed:
				}
			}
		}
	}()

	// Consumer: take 5 records, then cancel the pipeline.
	for i := 0; i < 5; i++ {
		fmt.Println("stored:", <-transformOut)
	}

	close(done)
	wg.Wait()
	fmt.Println("pipeline shut down cleanly")
}
```

Both stages check the same done channel. The `sync.WaitGroup` ensures main waits for all stages to finish cleanup before exiting.

### Verification
```
stored: transformed(record-0)
stored: transformed(record-1)
stored: transformed(record-2)
stored: transformed(record-3)
stored: transformed(record-4)
fetcher: cancelled, closing connection
transformer: cancelled, flushing buffer
pipeline shut down cleanly
```

## Step 4 -- Graceful Cleanup After Cancellation

The worker performs cleanup work after receiving the done signal: flushing in-memory data to disk, closing connections, and reporting final statistics. This demonstrates that done is a signal, not a kill.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)

		var cachedItems []string
		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				fmt.Printf("worker: cancellation received, flushing %d items\n", len(cachedItems))
				for _, item := range cachedItems {
					fmt.Printf("  flushed to disk: %s\n", item)
				}
				fmt.Println("worker: connections closed, cleanup complete")
				return
			case <-ticker.C:
				item := fmt.Sprintf("user-session-%d", len(cachedItems)+1)
				cachedItems = append(cachedItems, item)
				fmt.Printf("worker: cached %s\n", item)
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	fmt.Println("main: sending shutdown signal")
	close(done)
	<-finished
	fmt.Println("main: worker exited after cleanup")
}
```

### Verification
```
worker: cached user-session-1
worker: cached user-session-2
worker: cached user-session-3
worker: cached user-session-4
main: sending shutdown signal
worker: cancellation received, flushing 4 items
  flushed to disk: user-session-1
  flushed to disk: user-session-2
  flushed to disk: user-session-3
  flushed to disk: user-session-4
worker: connections closed, cleanup complete
main: worker exited after cleanup
```

## Common Mistakes

### 1. Sending a Value Instead of Closing
Sending a value on the done channel only wakes one receiver. If you have 5 workers, you need to send 5 values. Closing wakes ALL receivers:

```go
// BAD: only wakes one goroutine.
done <- struct{}{}

// GOOD: wakes all goroutines.
close(done)
```

### 2. Using chan bool Instead of chan struct{}
Both work, but `chan struct{}` communicates intent: this channel carries a signal, not data. It also has zero allocation cost per element:

```go
// Acceptable but unclear intent.
done := make(chan bool)

// Preferred: zero-size signal.
done := make(chan struct{})
```

### 3. Checking Done Outside of Select
A direct `<-done` blocks until the channel is closed. It must be inside a `select` alongside the work channel so the goroutine can do work while also being responsive to cancellation:

```go
// BAD: blocks until done is closed. Cannot do work.
<-done

// GOOD: checks done alongside work.
select {
case <-done:
    return
case results <- value:
}
```

### 4. Forgetting Done on Both Sides of a Pipeline Stage
A stage that reads from input and writes to output needs done checks on BOTH operations. Otherwise, it can block on a write after cancellation:

```go
// BAD: can block on the send after done is closed.
case record := <-input:
    output <- transform(record)

// GOOD: checks done on the send.
case record, ok := <-input:
    if !ok {
        return
    }
    select {
    case <-done:
        return
    case output <- transform(record):
    }
```

## Verify What You Learned

- [ ] Can you explain why close is a broadcast and send is not?
- [ ] Can you explain why `chan struct{}` is preferred over `chan bool`?
- [ ] Can you describe how to propagate cancellation through a multi-stage pipeline?
- [ ] Can you identify where `context.Context` replaces this pattern?

## What's Next
In the next exercise, you will build a heartbeat mechanism using `select` and `time.Ticker` to monitor whether goroutines are alive and responsive.

## Summary
The done channel pattern uses a closed `chan struct{}` as a broadcast cancellation signal. Closing the channel wakes all goroutines that check it in their `select` loops. In a cache refresher scenario, this lets you stop background workers cleanly: they finish their current operation, flush pending data, close connections, and exit. This is the manual implementation of what `context.Context` provides. Use `sync.WaitGroup` to wait for all goroutines to finish cleanup before the application exits.

## Reference
- [Go Concurrency Patterns: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go Spec: Close](https://go.dev/ref/spec#Close)
- [context package](https://pkg.go.dev/context)
