---
difficulty: advanced
concepts: [tee channel, stream splitting, nil-channel select, backpressure, data duplication]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, channels, select, done channel pattern, pipeline]
---

# 8. Tee-Channel: Split Stream

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** a tee function that duplicates a channel stream into two outputs
- **Explain** the nil-channel select trick for ensuring both outputs receive each value
- **Analyze** how backpressure from a slow consumer affects the entire tee
- **Apply** stream splitting for parallel processing of the same data

## Why Tee-Channel

The tee-channel pattern takes one input stream and duplicates it into two output streams. Every value from the input appears in both outputs. This is analogous to the Unix `tee` command, which reads from stdin and writes to both stdout and a file simultaneously.

Consider a real scenario: your application processes a stream of user events (purchases, clicks, signups). Every event must go to two destinations: an audit log (for compliance -- every event must be recorded permanently) and a real-time analytics processor (for live dashboards). You cannot lose events from either stream. The tee pattern guarantees that both consumers receive every single event, even if one consumer is slower than the other.

The challenge is backpressure. Since both output channels must receive every value, the tee runs at the speed of the slowest consumer. If the analytics processor is slow, the audit logger also slows down because the tee cannot send the next value until both consumers have received the current one.

```
  Event Processor with Tee

               +---> auditLog (every event, persistent)
  events ----> |
               +---> analytics (every event, real-time)

  Every event goes to BOTH outputs.
  Speed = min(auditLog speed, analytics speed)
```

## Step 1 -- Basic Tee Function with Nil-Channel Select

The nil-channel select pattern is the key technique for ensuring both outputs receive each value:

1. For each value from input, set `o1 = out1, o2 = out2` (both "armed")
2. Select: send to whichever consumer is ready first
3. Nil out the channel that received (`o1 = nil` or `o2 = nil`)
4. A nil channel blocks forever in select, so the next iteration MUST send to the other
5. After 2 sends, both consumers have the value

```go
package main

import (
	"fmt"
	"sync"
)

const eventTypePurchase = "purchase"

// Event represents a user action in the system.
type Event struct {
	ID     int
	Type   string
	UserID string
	Amount float64
}

// EventTee duplicates a single event stream into two independent outputs.
type EventTee struct {
	done chan struct{}
}

func NewEventTee() *EventTee {
	return &EventTee{done: make(chan struct{})}
}

func (et *EventTee) Split(in <-chan Event) (<-chan Event, <-chan Event) {
	out1 := make(chan Event)
	out2 := make(chan Event)

	go func() {
		defer close(out1)
		defer close(out2)

		for val := range in {
			o1, o2 := out1, out2
			for count := 0; count < 2; count++ {
				select {
				case o1 <- val:
					o1 = nil
				case o2 <- val:
					o2 = nil
				case <-et.done:
					return
				}
			}
		}
	}()

	return out1, out2
}

func (et *EventTee) Close() {
	close(et.done)
}

func emitEvents(events []Event) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		for _, e := range events {
			out <- e
		}
	}()
	return out
}

func runAuditLogger(stream <-chan Event, wg *sync.WaitGroup) {
	defer wg.Done()
	for event := range stream {
		fmt.Printf("  [AUDIT] event=%d type=%s user=%s\n",
			event.ID, event.Type, event.UserID)
	}
}

func runAnalyticsProcessor(stream <-chan Event, wg *sync.WaitGroup) {
	defer wg.Done()
	var totalRevenue float64
	for event := range stream {
		if event.Type == eventTypePurchase {
			totalRevenue += event.Amount
			fmt.Printf("  [ANALYTICS] purchase: $%.2f from %s (running total: $%.2f)\n",
				event.Amount, event.UserID, totalRevenue)
		}
	}
	fmt.Printf("  [ANALYTICS] session revenue: $%.2f\n", totalRevenue)
}

func main() {
	events := emitEvents([]Event{
		{ID: 1, Type: "purchase", UserID: "alice", Amount: 99.99},
		{ID: 2, Type: "signup", UserID: "bob", Amount: 0},
		{ID: 3, Type: "purchase", UserID: "charlie", Amount: 249.50},
		{ID: 4, Type: "click", UserID: "alice", Amount: 0},
		{ID: 5, Type: "purchase", UserID: "diana", Amount: 15.00},
	})

	tee := NewEventTee()
	auditStream, analyticsStream := tee.Split(events)

	var wg sync.WaitGroup
	wg.Add(2)
	go runAuditLogger(auditStream, &wg)
	go runAnalyticsProcessor(analyticsStream, &wg)

	wg.Wait()
	tee.Close()
	fmt.Println("\n  Both consumers received every event.")
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: audit logs all 5 events, analytics processes only purchases:
```
  [AUDIT] event=1 type=purchase user=alice
  [ANALYTICS] purchase: $99.99 from alice (running total: $99.99)
  [AUDIT] event=2 type=signup user=bob
  [AUDIT] event=3 type=purchase user=charlie
  [ANALYTICS] purchase: $249.50 from charlie (running total: $349.49)
  [AUDIT] event=4 type=click user=alice
  [AUDIT] event=5 type=purchase user=diana
  [ANALYTICS] purchase: $15.00 from diana (running total: $364.49)
  [ANALYTICS] session revenue: $364.49

  Both consumers received every event.
```

## Step 2 -- Backpressure Demonstration

Show how a slow consumer (e.g., a slow audit logger writing to disk) affects the fast consumer (real-time analytics).

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const slowConsumerDelay = 200 * time.Millisecond

// Event represents a user action in the system.
type Event struct {
	ID   int
	Type string
}

// EventTee duplicates a single event stream into two independent outputs.
type EventTee struct {
	done chan struct{}
}

func NewEventTee() *EventTee {
	return &EventTee{done: make(chan struct{})}
}

func (et *EventTee) Split(in <-chan Event) (<-chan Event, <-chan Event) {
	out1 := make(chan Event)
	out2 := make(chan Event)
	go func() {
		defer close(out1)
		defer close(out2)
		for val := range in {
			o1, o2 := out1, out2
			for count := 0; count < 2; count++ {
				select {
				case o1 <- val:
					o1 = nil
				case o2 <- val:
					o2 = nil
				case <-et.done:
					return
				}
			}
		}
	}()
	return out1, out2
}

func (et *EventTee) Close() {
	close(et.done)
}

func emitTimedEvents(count int) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		for i := 1; i <= count; i++ {
			fmt.Printf("  [source] emitting event %d at %v\n",
				i, time.Now().Format("04:05.000"))
			out <- Event{ID: i, Type: "purchase"}
		}
	}()
	return out
}

func runFastConsumer(stream <-chan Event, wg *sync.WaitGroup) {
	defer wg.Done()
	for event := range stream {
		fmt.Printf("  [analytics] got event %d at %v (fast)\n",
			event.ID, time.Now().Format("04:05.000"))
	}
}

func runSlowConsumer(stream <-chan Event, wg *sync.WaitGroup) {
	defer wg.Done()
	for event := range stream {
		fmt.Printf("  [audit]     got event %d at %v (slow - writing to disk...)\n",
			event.ID, time.Now().Format("04:05.000"))
		time.Sleep(slowConsumerDelay)
	}
}

func main() {
	tee := NewEventTee()
	defer tee.Close()

	events := emitTimedEvents(5)
	auditStream, analyticsStream := tee.Split(events)

	var wg sync.WaitGroup
	wg.Add(2)
	go runFastConsumer(analyticsStream, &wg)
	go runSlowConsumer(auditStream, &wg)

	wg.Wait()
	fmt.Println("\n  Notice: the fast analytics consumer was slowed down by the slow audit consumer.")
	fmt.Println("  The tee runs at the speed of the slowest consumer.")
}
```

### Intermediate Verification
```bash
go run main.go
```
Observe that the fast consumer receives values at the same pace as the slow one. The timestamps reveal the bottleneck.

## Step 3 -- Buffered Tee for Slow Consumer Decoupling

Mitigate backpressure by adding a buffer between the tee and the slow consumer. This decouples the two consumers up to the buffer capacity.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	slowAuditDelay  = 100 * time.Millisecond
	auditBufferSize = 5
	eventCount      = 8
)

// Event represents a user action in the system.
type Event struct {
	ID   int
	Type string
}

// BufferedEventTee splits a stream with buffered intermediate channels.
type BufferedEventTee struct {
	done chan struct{}
}

func NewBufferedEventTee() *BufferedEventTee {
	return &BufferedEventTee{done: make(chan struct{})}
}

func (bt *BufferedEventTee) splitRaw(in <-chan Event) (<-chan Event, <-chan Event) {
	raw1 := make(chan Event)
	raw2 := make(chan Event)
	go func() {
		defer close(raw1)
		defer close(raw2)
		for val := range in {
			o1, o2 := raw1, raw2
			for count := 0; count < 2; count++ {
				select {
				case o1 <- val:
					o1 = nil
				case o2 <- val:
					o2 = nil
				case <-bt.done:
					return
				}
			}
		}
	}()
	return raw1, raw2
}

func bufferChannel(in <-chan Event, size int) <-chan Event {
	buffered := make(chan Event, size)
	go func() {
		defer close(buffered)
		for v := range in {
			buffered <- v
		}
	}()
	return buffered
}

func (bt *BufferedEventTee) Split(in <-chan Event, buf1, buf2 int) (<-chan Event, <-chan Event) {
	raw1, raw2 := bt.splitRaw(in)
	return bufferChannel(raw1, buf1), bufferChannel(raw2, buf2)
}

func (bt *BufferedEventTee) Close() {
	close(bt.done)
}

func emitEvents(count int) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		for i := 1; i <= count; i++ {
			out <- Event{ID: i, Type: "event"}
		}
	}()
	return out
}

func main() {
	tee := NewBufferedEventTee()
	defer tee.Close()

	fmt.Println("=== Buffered Tee: Decoupling Slow Consumer ===")
	fmt.Println()

	events := emitEvents(eventCount)
	analyticsStream, auditStream := tee.Split(events, 0, auditBufferSize)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for event := range analyticsStream {
			fmt.Printf("  [analytics] event %d at %v\n",
				event.ID, time.Now().Format("04:05.000"))
		}
		fmt.Println("  [analytics] done")
	}()

	go func() {
		defer wg.Done()
		for event := range auditStream {
			fmt.Printf("  [audit]     event %d at %v (writing...)\n",
				event.ID, time.Now().Format("04:05.000"))
			time.Sleep(slowAuditDelay)
		}
		fmt.Println("  [audit]     done")
	}()

	wg.Wait()
	fmt.Println("\n  With buffering, the fast consumer finishes early.")
	fmt.Println("  The slow consumer continues processing from its buffer.")
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: analytics finishes faster than audit, decoupled by the buffer:
```
=== Buffered Tee: Decoupling Slow Consumer ===

  [analytics] event 1 at 00:01.000
  [audit]     event 1 at 00:01.000 (writing...)
  [analytics] event 2 at 00:01.000
  [analytics] event 3 at 00:01.001
  ...
  [analytics] done
  [audit]     event 5 at 00:01.400 (writing...)
  ...
  [audit]     done

  With buffering, the fast consumer finishes early.
  The slow consumer continues processing from its buffer.
```

## Step 4 -- Full Event Processing System

Build a complete event processor that tees events to both audit and analytics, with the analytics stream doing aggregation.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// Event represents a user action with financial data.
type Event struct {
	ID        int
	Type      string
	UserID    string
	Amount    float64
	Timestamp time.Time
}

// EventTee duplicates a single event stream into two independent outputs.
type EventTee struct {
	done chan struct{}
}

func NewEventTee() *EventTee {
	return &EventTee{done: make(chan struct{})}
}

func (et *EventTee) Split(in <-chan Event) (<-chan Event, <-chan Event) {
	out1 := make(chan Event)
	out2 := make(chan Event)
	go func() {
		defer close(out1)
		defer close(out2)
		for val := range in {
			o1, o2 := out1, out2
			for count := 0; count < 2; count++ {
				select {
				case o1 <- val:
					o1 = nil
				case o2 <- val:
					o2 = nil
				case <-et.done:
					return
				}
			}
		}
	}()
	return out1, out2
}

func (et *EventTee) Close() {
	close(et.done)
}

func buildSampleEvents() []Event {
	now := time.Now()
	return []Event{
		{1, "purchase", "alice", 99.99, now},
		{2, "signup", "bob", 0, now.Add(time.Second)},
		{3, "purchase", "charlie", 249.50, now.Add(2 * time.Second)},
		{4, "purchase", "alice", 35.00, now.Add(3 * time.Second)},
		{5, "refund", "charlie", -249.50, now.Add(4 * time.Second)},
		{6, "click", "diana", 0, now.Add(5 * time.Second)},
		{7, "purchase", "bob", 150.00, now.Add(6 * time.Second)},
		{8, "purchase", "diana", 75.25, now.Add(7 * time.Second)},
	}
}

func emitEvents(data []Event) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)
		for _, e := range data {
			out <- e
		}
	}()
	return out
}

func runAuditLogger(stream <-chan Event, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println("=== Audit Log ===")
	for e := range stream {
		fmt.Printf("  [%s] id=%d type=%-10s user=%-8s amount=%8.2f\n",
			e.Timestamp.Format("15:04:05"), e.ID, e.Type, e.UserID, e.Amount)
	}
}

func runAnalyticsAggregator(stream <-chan Event, wg *sync.WaitGroup) {
	defer wg.Done()
	userSpend := make(map[string]float64)
	typeCounts := make(map[string]int)

	for e := range stream {
		typeCounts[e.Type]++
		if e.Amount != 0 {
			userSpend[e.UserID] += e.Amount
		}
	}

	fmt.Println("\n=== Real-Time Analytics Summary ===")
	fmt.Println("  Event counts:")
	for t, c := range typeCounts {
		fmt.Printf("    %-10s: %d\n", t, c)
	}
	fmt.Println("  Revenue by user:")
	for u, s := range userSpend {
		fmt.Printf("    %-8s: $%.2f\n", u, s)
	}
}

func main() {
	tee := NewEventTee()
	events := emitEvents(buildSampleEvents())

	auditStream, analyticsStream := tee.Split(events)

	var wg sync.WaitGroup
	wg.Add(2)
	go runAuditLogger(auditStream, &wg)
	go runAnalyticsAggregator(analyticsStream, &wg)

	wg.Wait()
	tee.Close()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: complete audit log and analytics summary:
```
=== Audit Log ===
  [10:00:00] id=1 type=purchase   user=alice    amount=   99.99
  [10:00:01] id=2 type=signup     user=bob      amount=    0.00
  [10:00:02] id=3 type=purchase   user=charlie  amount=  249.50
  [10:00:03] id=4 type=purchase   user=alice    amount=   35.00
  [10:00:04] id=5 type=refund     user=charlie  amount= -249.50
  [10:00:05] id=6 type=click      user=diana    amount=    0.00
  [10:00:06] id=7 type=purchase   user=bob      amount=  150.00
  [10:00:07] id=8 type=purchase   user=diana    amount=   75.25

=== Real-Time Analytics Summary ===
  Event counts:
    purchase  : 5
    signup    : 1
    refund    : 1
    click     : 1
  Revenue by user:
    alice   : $134.99
    charlie : $0.00
    bob     : $150.00
    diana   : $75.25
```

## Common Mistakes

### Sending to Both Channels Without Coordination
**Wrong:**
```go
for val := range in {
	out1 <- val
	out2 <- val // blocks if out2 consumer is not ready
}
```
**What happens:** If `out2`'s consumer blocks, the send to `out1` in the next iteration also blocks, even if `out1`'s consumer is ready. Worse, there is no cancellation path.

**Fix:** Use `select` with nil-channel trick and done-channel, as shown in Step 1.

### Forgetting Done Channel in the Tee
**Wrong:**
```go
go func() {
	for val := range in {
		out1 <- val
		out2 <- val
	}
}()
```
**What happens:** If a consumer stops reading (context canceled, error, etc.), the tee goroutine blocks forever.

**Fix:** Always include `<-done` in select cases so the tee can exit when signaled.

### Closing Output Channels from the Consumer Side
Channels should be closed by the sender, not the receiver. The tee owns the output channels and closes them. Consumers should never close them.

## Verify What You Learned

Run `go run main.go` and verify:
- Basic tee: both audit and analytics receive all events
- Backpressure demo: fast consumer paced by slow consumer (timestamps prove it)
- Buffered tee: fast consumer finishes before slow consumer
- Full system: audit log has all 8 events, analytics summary matches expected aggregations

## What's Next
Continue to [09-rate-limiter-token-bucket](../09-rate-limiter-token-bucket/09-rate-limiter-token-bucket.md) to learn how to control the rate of work processing.

## Summary
- The tee-channel duplicates one input stream into two output streams
- Use the nil-channel select pattern to ensure both outputs receive each value
- The tee runs at the speed of the slowest consumer (backpressure)
- Add buffered intermediate channels to decouple fast and slow consumers
- Always include a `done` channel for cancellation to prevent goroutine leaks
- Real-world use: sending events to both audit logging and real-time analytics simultaneously

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
- [Concurrency in Go (Katherine Cox-Buday)](https://www.oreilly.com/library/view/concurrency-in-go/) -- tee-channel pattern
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
