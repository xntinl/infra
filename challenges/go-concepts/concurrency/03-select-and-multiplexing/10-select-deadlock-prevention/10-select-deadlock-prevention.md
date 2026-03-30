---
difficulty: advanced
concepts: [select, deadlock, default-case, timeout, done-channel, goroutine-leak, blocking-prevention]
tools: [go]
estimated_time: 40m
bloom_level: analyze
---

# 10. Select for Deadlock Prevention

## Learning Objectives
- **Identify** common deadlock scenarios with channels and explain why they occur
- **Apply** `select` with `default` to avoid blocking on full or empty channels
- **Use** `select` with `time.After` to break out of stuck operations
- **Build** goroutines that always have an exit path via done channels

## Why Select Prevents Deadlocks

A deadlock happens when a goroutine is permanently blocked waiting for a channel operation that will never complete. In Go, the runtime detects when all goroutines are blocked and panics with `fatal error: all goroutines are asleep - deadlock!`. But partial deadlocks -- where some goroutines hang while others continue -- are silent. They leak memory, consume goroutine slots, and slowly degrade the system.

Three common causes of channel deadlocks:
1. **Sending to a full channel** when no receiver exists or the receiver has stopped.
2. **Receiving from an empty channel** when no sender exists or the sender has stopped.
3. **Circular dependencies** where goroutine A waits for B, and B waits for A.

The `select` statement is Go's primary tool for breaking these deadlocks. It provides three escape mechanisms:
- `default` makes a channel operation non-blocking.
- `time.After` (or `time.NewTimer`) imposes a maximum wait duration.
- A `done` channel provides an external cancellation signal.

The rule is simple: **every channel operation inside a long-running goroutine should have an exit path.** If it can block, wrap it in a `select` with at least one alternative case.

## Step 1 -- The Deadlock: Sending with No Receiver

Demonstrate the simplest deadlock: a goroutine tries to send to a channel that nobody is reading. Then fix it with `select` and `default`.

```go
package main

import (
	"fmt"
	"time"
)

type Event struct {
	Name    string
	Payload string
}

type EventBus struct {
	subscribers chan Event
	dropped     int
}

func NewEventBus(bufferSize int) *EventBus {
	return &EventBus{
		subscribers: make(chan Event, bufferSize),
	}
}

func (eb *EventBus) PublishBlocking(event Event) {
	// This blocks if the buffer is full and no subscriber is reading.
	// In production, a slow subscriber causes the publisher to hang.
	eb.subscribers <- event
	fmt.Printf("published (blocking): %s\n", event.Name)
}

func (eb *EventBus) PublishSafe(event Event) {
	select {
	case eb.subscribers <- event:
		fmt.Printf("published: %s\n", event.Name)
	default:
		eb.dropped++
		fmt.Printf("dropped (buffer full): %s [total dropped: %d]\n", event.Name, eb.dropped)
	}
}

func (eb *EventBus) Subscribe() <-chan Event {
	return eb.subscribers
}

func main() {
	bus := NewEventBus(3)

	fmt.Println("=== Safe publish with select+default ===")
	events := []Event{
		{Name: "user.created", Payload: "user-1"},
		{Name: "user.updated", Payload: "user-1"},
		{Name: "order.placed", Payload: "order-42"},
		{Name: "order.shipped", Payload: "order-42"},
		{Name: "payment.received", Payload: "pay-99"},
		{Name: "user.deleted", Payload: "user-1"},
	}

	for _, e := range events {
		bus.PublishSafe(e)
	}

	fmt.Println("\n=== Draining subscriber channel ===")
	close(bus.subscribers)
	for event := range bus.Subscribe() {
		fmt.Printf("consumed: %s\n", event.Name)
	}

	fmt.Printf("\nresult: %d events dropped because buffer was full\n", bus.dropped)

	// Uncomment the following to see the deadlock:
	// bus2 := NewEventBus(2)
	// bus2.PublishBlocking(Event{Name: "a", Payload: "1"})
	// bus2.PublishBlocking(Event{Name: "b", Payload: "2"})
	// bus2.PublishBlocking(Event{Name: "c", Payload: "3"}) // DEADLOCK: buffer full, no reader.

	fmt.Println("\n--- How default prevents deadlock ---")
	fmt.Println("Without select+default: goroutine blocks forever on full channel.")
	fmt.Println("With select+default: send is skipped, event is dropped, goroutine continues.")

	time.Sleep(10 * time.Millisecond)
}
```

The `PublishSafe` method uses `select` with `default` to make the send non-blocking. If the buffer is full, the event is dropped instead of blocking the publisher. This is the standard pattern for event buses, log dispatchers, and metrics emitters where dropping data is preferable to hanging.

### Verification
```
=== Safe publish with select+default ===
published: user.created
published: user.updated
published: order.placed
dropped (buffer full): order.shipped [total dropped: 1]
dropped (buffer full): payment.received [total dropped: 2]
dropped (buffer full): user.deleted [total dropped: 3]

=== Draining subscriber channel ===
consumed: user.created
consumed: user.updated
consumed: order.placed

result: 3 events dropped because buffer was full

--- How default prevents deadlock ---
Without select+default: goroutine blocks forever on full channel.
With select+default: send is skipped, event is dropped, goroutine continues.
```
Three events fit in the buffer. The rest are dropped safely.

## Step 2 -- Timeout Breaks Stuck Operations

Build a service that calls a slow backend. Without a timeout, a stuck backend blocks the caller forever. Use `select` with `time.After` to enforce a maximum wait.

```go
package main

import (
	"fmt"
	"time"
)

type BackendResponse struct {
	Data   string
	Source string
}

func callSlowBackend(name string, latency time.Duration) <-chan BackendResponse {
	ch := make(chan BackendResponse, 1)
	go func() {
		time.Sleep(latency)
		ch <- BackendResponse{Data: fmt.Sprintf("data from %s", name), Source: name}
	}()
	return ch
}

func fetchWithTimeout(name string, latency, timeout time.Duration) (BackendResponse, error) {
	responseCh := callSlowBackend(name, latency)

	select {
	case resp := <-responseCh:
		return resp, nil
	case <-time.After(timeout):
		return BackendResponse{}, fmt.Errorf("timeout waiting for %s after %v", name, timeout)
	}
}

type AggregatedResult struct {
	Responses []BackendResponse
	Errors    []string
}

func fetchAll(backends map[string]time.Duration, timeout time.Duration) AggregatedResult {
	type result struct {
		resp BackendResponse
		err  error
	}

	results := make(chan result, len(backends))

	for name, latency := range backends {
		go func(n string, lat time.Duration) {
			resp, err := fetchWithTimeout(n, lat, timeout)
			results <- result{resp: resp, err: err}
		}(name, latency)
	}

	var agg AggregatedResult
	for i := 0; i < len(backends); i++ {
		r := <-results
		if r.err != nil {
			agg.Errors = append(agg.Errors, r.err.Error())
		} else {
			agg.Responses = append(agg.Responses, r.resp)
		}
	}
	return agg
}

func main() {
	backends := map[string]time.Duration{
		"user-service":    30 * time.Millisecond,
		"payment-service": 200 * time.Millisecond,
		"inventory-api":   50 * time.Millisecond,
		"analytics":       500 * time.Millisecond,
	}

	timeout := 100 * time.Millisecond
	fmt.Printf("fetching from %d backends with %v timeout\n\n", len(backends), timeout)

	result := fetchAll(backends, timeout)

	fmt.Println("=== Successful Responses ===")
	for _, resp := range result.Responses {
		fmt.Printf("  %s: %s\n", resp.Source, resp.Data)
	}

	fmt.Println("\n=== Timeouts ===")
	for _, err := range result.Errors {
		fmt.Printf("  %s\n", err)
	}

	fmt.Printf("\nsummary: %d succeeded, %d timed out\n", len(result.Responses), len(result.Errors))
}
```

`fetchWithTimeout` wraps every backend call in a `select` with `time.After`. Fast backends return before the timeout. Slow backends are abandoned (their goroutine completes eventually but the result is discarded). This is how API gateways prevent one slow dependency from blocking the entire request.

### Verification
```
fetching from 4 backends with 100ms timeout

=== Successful Responses ===
  user-service: data from user-service
  inventory-api: data from inventory-api

=== Timeouts ===
  timeout waiting for payment-service after 100ms
  timeout waiting for analytics after 100ms

summary: 2 succeeded, 2 timed out
```
Fast backends respond. Slow backends are timed out without blocking the caller.

## Step 3 -- Done Channel Breaks Circular Blocking

Build a pipeline where two stages can deadlock if one stops consuming. Use a done channel to give every goroutine an escape route.

```go
package main

import (
	"fmt"
	"time"
)

func producer(done <-chan struct{}, items []string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for _, item := range items {
			select {
			case <-done:
				fmt.Println("producer: received done signal, exiting")
				return
			case out <- item:
				fmt.Printf("producer: sent %q\n", item)
			}
		}
	}()
	return out
}

func transformer(done <-chan struct{}, input <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				fmt.Println("transformer: received done signal, exiting")
				return
			case item, ok := <-input:
				if !ok {
					fmt.Println("transformer: input closed, exiting")
					return
				}
				result := fmt.Sprintf("[transformed] %s", item)
				select {
				case <-done:
					fmt.Println("transformer: received done signal during send, exiting")
					return
				case out <- result:
				}
			}
		}
	}()
	return out
}

func main() {
	fmt.Println("=== Pipeline with done channel ===")
	fmt.Println("Consumer reads only 3 items, then signals done.")
	fmt.Println("Without done channel: producer and transformer block forever.")
	fmt.Println()

	done := make(chan struct{})

	items := []string{"order-1", "order-2", "order-3", "order-4", "order-5",
		"order-6", "order-7", "order-8", "order-9", "order-10"}

	stage1 := producer(done, items)
	stage2 := transformer(done, stage1)

	consumed := 0
	for item := range stage2 {
		fmt.Printf("consumer: received %s\n", item)
		consumed++
		if consumed >= 3 {
			fmt.Println("\nconsumer: got enough, signaling done")
			close(done)
			break
		}
	}

	time.Sleep(50 * time.Millisecond)
	fmt.Printf("\nresult: consumed %d items, pipeline shut down cleanly\n", consumed)
}
```

Without the done channel, when the consumer stops reading after 3 items, the transformer blocks trying to send to `stage2`, and the producer blocks trying to send to `stage1`. Both goroutines leak. With the done channel, both stages detect the signal and exit.

### Verification
```
=== Pipeline with done channel ===
Consumer reads only 3 items, then signals done.
Without done channel: producer and transformer block forever.

producer: sent "order-1"
producer: sent "order-2"
consumer: received [transformed] order-1
producer: sent "order-3"
consumer: received [transformed] order-2
consumer: received [transformed] order-3

consumer: got enough, signaling done
producer: received done signal, exiting
transformer: received done signal, exiting

result: consumed 3 items, pipeline shut down cleanly
```
Both pipeline stages exit cleanly despite having more items to process.

## Step 4 -- Pipeline Coordinator with All Three Protections

Combine all three techniques into a pipeline coordinator that is resilient to every common deadlock scenario: slow stages, full buffers, and external shutdown.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type PipelineConfig struct {
	BufferSize     int
	StageTimeout   time.Duration
	MaxItems       int
}

type StageStats struct {
	Name      string
	Processed int
	Dropped   int
	TimedOut  int
}

type Coordinator struct {
	config PipelineConfig
	done   chan struct{}
	mu     sync.Mutex
	stats  map[string]*StageStats
}

func NewCoordinator(config PipelineConfig) *Coordinator {
	return &Coordinator{
		config: config,
		done:   make(chan struct{}),
		stats:  make(map[string]*StageStats),
	}
}

func (c *Coordinator) recordStat(name, field string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.stats[name]
	if !ok {
		s = &StageStats{Name: name}
		c.stats[name] = s
	}
	switch field {
	case "processed":
		s.Processed++
	case "dropped":
		s.Dropped++
	case "timedout":
		s.TimedOut++
	}
}

func (c *Coordinator) ingest(items []string) <-chan string {
	out := make(chan string, c.config.BufferSize)
	go func() {
		defer close(out)
		for _, item := range items {
			select {
			case <-c.done:
				return
			case out <- item:
				c.recordStat("ingest", "processed")
			default:
				c.recordStat("ingest", "dropped")
			}
		}
	}()
	return out
}

func (c *Coordinator) transform(input <-chan string) <-chan string {
	out := make(chan string, c.config.BufferSize)
	go func() {
		defer close(out)
		for {
			select {
			case <-c.done:
				return
			case item, ok := <-input:
				if !ok {
					return
				}
				// Simulate variable processing time.
				time.Sleep(15 * time.Millisecond)
				result := fmt.Sprintf("processed(%s)", item)

				select {
				case <-c.done:
					return
				case out <- result:
					c.recordStat("transform", "processed")
				case <-time.After(c.config.StageTimeout):
					c.recordStat("transform", "timedout")
				}
			}
		}
	}()
	return out
}

func (c *Coordinator) collect(input <-chan string) []string {
	var results []string
	for {
		select {
		case <-c.done:
			return results
		case item, ok := <-input:
			if !ok {
				return results
			}
			results = append(results, item)
			c.recordStat("collect", "processed")
			if len(results) >= c.config.MaxItems {
				return results
			}
		case <-time.After(c.config.StageTimeout):
			c.recordStat("collect", "timedout")
			return results
		}
	}
}

func (c *Coordinator) Shutdown() {
	close(c.done)
}

func (c *Coordinator) PrintStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Println("\n=== Pipeline Stats ===")
	for _, s := range c.stats {
		fmt.Printf("  %-12s processed=%d dropped=%d timedout=%d\n",
			s.Name, s.Processed, s.Dropped, s.TimedOut)
	}
}

func main() {
	config := PipelineConfig{
		BufferSize:   5,
		StageTimeout: 100 * time.Millisecond,
		MaxItems:     8,
	}

	coord := NewCoordinator(config)

	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf("item-%d", i)
	}

	fmt.Printf("pipeline: %d items, buffer=%d, timeout=%v, max=%d\n",
		len(items), config.BufferSize, config.StageTimeout, config.MaxItems)
	fmt.Println()

	stage1 := coord.ingest(items)
	stage2 := coord.transform(stage1)
	results := coord.collect(stage2)

	coord.Shutdown()

	fmt.Println("=== Collected Results ===")
	for _, r := range results {
		fmt.Printf("  %s\n", r)
	}

	coord.PrintStats()

	fmt.Println("\n=== Protection Summary ===")
	fmt.Println("  default:     ingest drops items when buffer is full (no block)")
	fmt.Println("  time.After:  transform/collect abandon stuck operations")
	fmt.Println("  done:        all stages exit on shutdown signal")
}
```

This coordinator uses all three deadlock prevention mechanisms:
- **Ingest** uses `select` with `default` to drop items when the buffer is full.
- **Transform** uses `select` with `time.After` to abandon stuck sends.
- **Collect** uses `select` with `time.After` to stop waiting for slow data.
- **All stages** check the `done` channel for shutdown.

### Verification
```
pipeline: 20 items, buffer=5, timeout=100ms, max=8

=== Collected Results ===
  processed(item-0)
  processed(item-1)
  processed(item-2)
  processed(item-3)
  processed(item-4)
  processed(item-5)
  processed(item-6)
  processed(item-7)

=== Pipeline Stats ===
  ingest       processed=9 dropped=11 timedout=0
  transform    processed=8 dropped=0 timedout=0
  collect      processed=8 dropped=0 timedout=0

=== Protection Summary ===
  default:     ingest drops items when buffer is full (no block)
  time.After:  transform/collect abandon stuck operations
  done:        all stages exit on shutdown signal
```
The exact counts vary, but the pipeline never deadlocks. Excess items are dropped, slow operations are timed out, and shutdown is clean.

## Intermediate Verification

Run each step with the race detector:
```bash
go run -race main.go
```
All steps should complete without deadlock or race warnings.

## Common Mistakes

### 1. Naked Channel Sends in Goroutines
A goroutine that sends to a channel without a `select` will block forever if the receiver disappears:

```go
// BAD: blocks forever if nobody reads from out.
go func() {
    out <- result
}()

// GOOD: can exit if done is signaled.
go func() {
    select {
    case <-done:
    case out <- result:
    }
}()
```

### 2. Using time.After in a Hot Loop
`time.After` creates a new timer on each call. In a tight loop, this leaks timers until garbage collection catches up:

```go
// BAD: creates a new timer every iteration.
for {
    select {
    case <-time.After(1 * time.Second):
    case v := <-ch:
        process(v)
    }
}

// GOOD: reuse the timer.
timer := time.NewTimer(1 * time.Second)
defer timer.Stop()
for {
    timer.Reset(1 * time.Second)
    select {
    case <-timer.C:
    case v := <-ch:
        process(v)
    }
}
```

### 3. Forgetting Default Means "Block Until One Case Is Ready"
A `select` without `default` blocks until at least one case can proceed. If none can, the goroutine hangs. Add `default` when non-blocking behavior is required, but remember that `default` means "never wait" -- it changes the semantics fundamentally.

### 4. Closing a Channel from the Receiver Side
Only the sender should close a channel. If the receiver closes it, the sender panics on the next send:

```go
// BAD: receiver closes the channel.
close(input) // sender will panic on next send.

// GOOD: use a done channel to signal the sender to stop.
close(done) // sender checks done and stops sending.
```

## Verify What You Learned

- [ ] Can you name the three `select` mechanisms for breaking deadlocks and when to use each?
- [ ] Can you explain why `default` in a send `select` drops data instead of blocking?
- [ ] Can you identify a goroutine that will leak without a done channel?
- [ ] Can you explain the timer leak problem with `time.After` in a loop and the fix?

## What's Next
You have completed the select and multiplexing section. Continue to [11-waitgroup-patterns](../../04-sync-primitives/11-waitgroup-patterns/) to learn advanced `sync.WaitGroup` patterns for coordinating parallel operations.

## Summary
Every channel operation in a long-running goroutine needs an exit path. `select` provides three escape mechanisms: `default` makes sends/receives non-blocking (use for event buses and metrics where dropping data is acceptable), `time.After` imposes a deadline on stuck operations (use for backend calls and pipeline stages), and a `done` channel provides external shutdown signaling (use for every goroutine that outlives a single request). The rule is: if a goroutine can block on a channel, it must have a way to unblock. Combine all three in pipeline coordinators for maximum resilience against deadlocks and goroutine leaks.

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
- [Go Concurrency Patterns: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Effective Go: Select](https://go.dev/doc/effective_go#select)
- [time.After documentation](https://pkg.go.dev/time#After)
