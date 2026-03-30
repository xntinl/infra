---
difficulty: intermediate
concepts: [channels, ownership, share by communicating, goroutine confinement, command pattern]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 4. Fix Race with Channel


## Learning Objectives
After completing this exercise, you will be able to:
- **Fix** a data race by funneling writes through a channel to a single owner goroutine
- **Apply** the Go proverb: "Don't communicate by sharing memory; share memory by communicating"
- **Build** a channel-based metrics collector that receives commands
- **Compare** the channel approach with the mutex approach in clarity and performance

## Why Channels

The mutex approach from exercise 03 works by allowing multiple goroutines to access shared memory, but serializing their access with locks. The channel approach takes a fundamentally different perspective: instead of sharing a variable and protecting it, you give **ownership** of the variable to a single goroutine and have all other goroutines communicate with it through channels.

This is the Go philosophy captured in the proverb: **"Don't communicate by sharing memory; share memory by communicating."**

When a single goroutine owns the data, there is no concurrent access, so there is no race. The channel serves as both the communication mechanism and the synchronization mechanism.

## Step 1 -- Channel-Based Hit Counter

Instead of locking a shared counter, send increment commands to a goroutine that owns the counter:

```go
package main

import (
	"fmt"
	"sync"
)

func channelHitCounter() int {
	increments := make(chan struct{}, 100)
	done := make(chan int)

	// Owner goroutine: the SOLE reader/writer of hitCount.
	go func() {
		hitCount := 0
		for range increments {
			hitCount++
		}
		done <- hitCount
	}()

	// Simulated HTTP handlers send increment signals.
	var wg sync.WaitGroup
	for handler := 0; handler < 100; handler++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := 0; req < 100; req++ {
				increments <- struct{}{}
			}
		}()
	}

	wg.Wait()
	close(increments)

	return <-done
}

func main() {
	result := channelHitCounter()
	fmt.Printf("Hit count: %d (expected 10000)\n", result)
}
```

Key observations:
- Only the owner goroutine reads and writes `hitCount`: no concurrent access
- `close(increments)` causes the `range` loop to exit
- `done <- hitCount` sends the final value back to the caller
- The buffered channel (capacity 100) reduces blocking

### Verification
```bash
go run -race main.go
```
Expected: 10000 with zero race warnings.

## Step 2 -- Channel-Based Metrics Collector

Build the same MetricsCollector from exercise 03, but using channels instead of a mutex. A single goroutine owns the map and processes commands sent through a channel:

```go
package main

import (
	"fmt"
	"sync"
)

type command struct {
	action   string
	endpoint string
	resultCh chan<- map[string]int
}

type ChannelMetrics struct {
	cmdCh chan command
}

func NewChannelMetrics() *ChannelMetrics {
	m := &ChannelMetrics{
		cmdCh: make(chan command, 256),
	}
	go m.run()
	return m
}

func (m *ChannelMetrics) run() {
	counters := make(map[string]int)
	for cmd := range m.cmdCh {
		switch cmd.action {
		case "record":
			counters[cmd.endpoint]++
		case "snapshot":
			snapshot := make(map[string]int, len(counters))
			for k, v := range counters {
				snapshot[k] = v
			}
			cmd.resultCh <- snapshot
		}
	}
}

func (m *ChannelMetrics) RecordRequest(endpoint string) {
	m.cmdCh <- command{action: "record", endpoint: endpoint}
}

func (m *ChannelMetrics) Snapshot() map[string]int {
	resultCh := make(chan map[string]int, 1)
	m.cmdCh <- command{action: "snapshot", resultCh: resultCh}
	return <-resultCh
}

func (m *ChannelMetrics) Close() {
	close(m.cmdCh)
}

func main() {
	metrics := NewChannelMetrics()
	var wg sync.WaitGroup

	endpoints := []string{"/api/users", "/api/orders", "/api/products", "/healthz"}

	for _, ep := range endpoints {
		for handler := 0; handler < 50; handler++ {
			wg.Add(1)
			go func(endpoint string) {
				defer wg.Done()
				for req := 0; req < 100; req++ {
					metrics.RecordRequest(endpoint)
				}
			}(ep)
		}
	}

	wg.Wait()

	fmt.Println("=== Channel-Based Metrics Collector ===")
	snapshot := metrics.Snapshot()
	total := 0
	for endpoint, count := range snapshot {
		fmt.Printf("  %-20s %d requests\n", endpoint, count)
		total += count
	}
	fmt.Printf("  %-20s %d requests\n", "TOTAL", total)
	fmt.Printf("\nExpected: 5000 per endpoint, 20000 total\n")

	metrics.Close()
}
```

This is the **command pattern**: callers send commands through a channel, and a single goroutine processes them sequentially. The map is never accessed concurrently because only one goroutine ever touches it.

For `Snapshot()`, the caller sends a command with a response channel. The owner processes the request, builds a copy of the map, and sends it back. This request-response pattern over channels is common in production Go code.

### Verification
```bash
go run -race main.go
```
Expected: 5000 per endpoint, 20000 total, zero race warnings.

## Step 3 -- Compare Mutex vs Channel

Both approaches solve the same problem. Which should you choose?

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func benchmarkMutex(handlers, reqs int) time.Duration {
	var mu sync.Mutex
	counters := make(map[string]int)
	var wg sync.WaitGroup

	start := time.Now()
	for h := 0; h < handlers; h++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ep := fmt.Sprintf("/api/endpoint-%d", id%4)
			for r := 0; r < reqs; r++ {
				mu.Lock()
				counters[ep]++
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()
	return time.Since(start)
}

func benchmarkChannel(handlers, reqs int) time.Duration {
	cmdCh := make(chan string, 256)
	done := make(chan struct{})

	go func() {
		counters := make(map[string]int)
		for ep := range cmdCh {
			counters[ep]++
		}
		close(done)
	}()

	var wg sync.WaitGroup
	start := time.Now()
	for h := 0; h < handlers; h++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ep := fmt.Sprintf("/api/endpoint-%d", id%4)
			for r := 0; r < reqs; r++ {
				cmdCh <- ep
			}
		}(h)
	}
	wg.Wait()
	close(cmdCh)
	<-done
	return time.Since(start)
}

func main() {
	fmt.Println("=== Mutex vs Channel Comparison ===")
	fmt.Println()

	handlers, reqs := 100, 1000

	mutexTime := benchmarkMutex(handlers, reqs)
	channelTime := benchmarkChannel(handlers, reqs)

	fmt.Printf("  Mutex:   %v\n", mutexTime)
	fmt.Printf("  Channel: %v\n", channelTime)
	fmt.Println()

	fmt.Println("When to use Mutex:")
	fmt.Println("  - Simple state protection (counters, maps)")
	fmt.Println("  - High-frequency updates where channel overhead matters")
	fmt.Println("  - Familiar lock-based reasoning")
	fmt.Println()
	fmt.Println("When to use Channel:")
	fmt.Println("  - Complex state machines with multiple operations")
	fmt.Println("  - When you want clear data ownership")
	fmt.Println("  - Pipeline architectures with stages")
	fmt.Println("  - When the command pattern makes the API clearer")
}
```

### Verification
```bash
go run main.go
```

For simple counters and maps, the mutex approach is typically faster because each channel send/receive has more overhead than a lock/unlock. The channel approach shines when the owned state is complex, the commands carry meaningful data, or the architecture is naturally a pipeline.

## Common Mistakes

### Forgetting to Close the Channel
```go
wg.Wait()
// forgot close(increments)
return <-done // DEADLOCK: owner is still ranging over increments
```
The owner goroutine blocks forever on `range increments`. Always `close(increments)` after all senders are done.

### Closing the Channel Before All Sends Complete
```go
go func() {
    defer wg.Done()
    for j := 0; j < 100; j++ {
        increments <- struct{}{}
    }
    close(increments) // BUG: other goroutines are still sending!
}()
```
Sending on a closed channel causes a **panic**. Close the channel once from the coordinating goroutine, after `wg.Wait()` confirms all senders have finished.

### Leaking the Owner Goroutine
If nothing ever closes the command channel, the owner goroutine runs forever. In a real server, call `Close()` during graceful shutdown to clean up.

### Not Considering Batching
For high-frequency counters, sending one signal per increment is expensive. Consider batching: accumulate counts locally and send a single batch update, or use a mutex instead.

## Verify What You Learned

1. Confirm zero race warnings with `go run -race main.go`
2. Why is there no race on `counters` in the channel-based metrics collector?
3. How does the `Snapshot()` method get data back from the owner goroutine?
4. When would you prefer the channel approach over a mutex?

## What's Next
Continue to [05-fix-race-with-atomic](../05-fix-race-with-atomic/05-fix-race-with-atomic.md) to fix simple counters using `sync/atomic` and compare all three approaches.

## Summary
- The channel approach eliminates races by giving data ownership to a single goroutine
- Worker goroutines communicate through channels instead of accessing shared memory
- The command pattern: callers send commands, owner processes them sequentially
- For request-response operations (like Snapshot), send a response channel with the command
- "Don't communicate by sharing memory; share memory by communicating"
- `close(channel)` must happen after all senders are done
- For simple counters, channels have more overhead than mutexes
- The channel pattern shines with complex state, command-based APIs, and pipeline architectures

## Reference
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Proverbs](https://go-proverbs.github.io/)
