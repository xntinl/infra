---
difficulty: advanced
concepts: [fan-in, merge, variadic-channels, WaitGroup, goroutine-per-channel, dynamic-multiplexing]
tools: [go]
estimated_time: 40m
bloom_level: create
---

# 8. Multiplexing N Sources

## Learning Objectives
- **Build** a merge function that combines N channels into one output channel
- **Use** goroutine-per-channel fan-in with proper cleanup via WaitGroup
- **Handle** source closure and output channel lifecycle correctly
- **Add** cancellation support to the merge pattern

## Why Multiplexing N Sources

Earlier exercises used `select` with a fixed number of channels. This works when the number of sources is known at compile time. But many real systems have a dynamic number of sources: you might aggregate logs from N application instances, collect metrics from a variable number of sensor feeds, or merge events from multiple API streams. The number of sources is only known at runtime.

You cannot write a `select` with a variable number of cases -- Go's `select` requires cases to be lexically present at compile time. The solution is the fan-in pattern: spawn one goroutine per source channel, each forwarding its values to a single shared output channel. A `sync.WaitGroup` tracks when all source goroutines have finished, at which point the output channel is closed.

This is the general-purpose channel multiplexer. It appears in Go's standard patterns, in the `x/sync/errgroup` package, and in virtually every pipeline-based architecture. Mastering it gives you the ability to compose arbitrary channel topologies.

## Step 1 -- Merge Two Log Streams

Start with the simplest case: merge log events from two application instances into a single ordered stream.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func merge(stream1, stream2 <-chan string) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	forward := func(ch <-chan string) {
		defer wg.Done()
		for event := range ch {
			out <- event
		}
	}

	wg.Add(2)
	go forward(stream1)
	go forward(stream2)

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	app1Logs := make(chan string)
	app2Logs := make(chan string)

	go func() {
		entries := []string{"app1: request received", "app1: db query 42ms", "app1: response 200"}
		for _, entry := range entries {
			app1Logs <- entry
			time.Sleep(30 * time.Millisecond)
		}
		close(app1Logs)
	}()

	go func() {
		entries := []string{"app2: request received", "app2: cache hit", "app2: response 200", "app2: request received", "app2: response 500"}
		for _, entry := range entries {
			app2Logs <- entry
			time.Sleep(20 * time.Millisecond)
		}
		close(app2Logs)
	}()

	for event := range merge(app1Logs, app2Logs) {
		fmt.Println(event)
	}
	fmt.Println("all log streams closed")
}
```

Each source gets its own goroutine that forwards events to `out`. When a source closes, `range` exits and `wg.Done()` is called. After all sources finish, the WaitGroup goroutine closes `out`, which terminates the consumer's `range`.

### Verification
```
app1: request received
app2: request received
app2: cache hit
app1: db query 42ms
app2: response 200
app2: request received
app1: response 200
app2: response 500
all log streams closed
```
The exact interleaving varies, but all 8 events appear and both streams are fully consumed.

## Step 2 -- Generalize to N Streams

Replace the two-stream merge with a variadic version that accepts any number of channels. This is what you need when the number of application instances is only known at runtime.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func mergeN(streams ...<-chan string) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	forward := func(ch <-chan string) {
		defer wg.Done()
		for event := range ch {
			out <- event
		}
	}

	wg.Add(len(streams))
	for _, ch := range streams {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	numInstances := 4
	streams := make([]<-chan string, numInstances)

	for i := 0; i < numInstances; i++ {
		ch := make(chan string)
		streams[i] = ch
		go func(instanceID int, c chan<- string) {
			for j := 0; j < 3; j++ {
				c <- fmt.Sprintf("instance-%d: event-%d", instanceID, j)
				time.Sleep(time.Duration(20*(instanceID+1)) * time.Millisecond)
			}
			close(c)
		}(i, ch)
	}

	for event := range mergeN(streams...) {
		fmt.Println(event)
	}
	fmt.Println("all instances reported, aggregation complete")
}
```

The pattern is identical to the two-stream version. The only change is iterating over the variadic slice instead of hardcoding two goroutines.

### Verification
```
instance-0: event-0
instance-1: event-0
instance-2: event-0
instance-3: event-0
instance-0: event-1
instance-1: event-1
...
all instances reported, aggregation complete
```
You should see 12 events (4 instances x 3 events each) in interleaved order.

## Step 3 -- Handle Sources Finishing at Different Times

In real systems, some sources produce data for minutes while others finish in seconds. The merge function must handle this gracefully: keep reading from active sources even after others close.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func mergeN(streams ...<-chan string) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	forward := func(ch <-chan string) {
		defer wg.Done()
		for event := range ch {
			out <- event
		}
	}

	wg.Add(len(streams))
	for _, ch := range streams {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	// Sensor A: sends 2 readings quickly, then closes.
	sensorA := make(chan string)
	go func() {
		sensorA <- "sensor-A: temperature=22.5C"
		time.Sleep(20 * time.Millisecond)
		sensorA <- "sensor-A: temperature=22.6C"
		close(sensorA)
		fmt.Println("--- sensor-A stream ended ---")
	}()

	// Sensor B: sends 5 readings over a longer period.
	sensorB := make(chan string)
	go func() {
		for i := 0; i < 5; i++ {
			sensorB <- fmt.Sprintf("sensor-B: pressure=%dhPa", 1013+i)
			time.Sleep(40 * time.Millisecond)
		}
		close(sensorB)
		fmt.Println("--- sensor-B stream ended ---")
	}()

	// Sensor C: sends 3 readings at medium pace.
	sensorC := make(chan string)
	go func() {
		for i := 0; i < 3; i++ {
			sensorC <- fmt.Sprintf("sensor-C: humidity=%d%%", 60+i)
			time.Sleep(50 * time.Millisecond)
		}
		close(sensorC)
		fmt.Println("--- sensor-C stream ended ---")
	}()

	for event := range mergeN(sensorA, sensorB, sensorC) {
		fmt.Println("aggregated:", event)
	}
	fmt.Println("all sensor streams closed")
}
```

### Verification
```
aggregated: sensor-A: temperature=22.5C
aggregated: sensor-B: pressure=1013hPa
aggregated: sensor-C: humidity=60%
aggregated: sensor-A: temperature=22.6C
--- sensor-A stream ended ---
aggregated: sensor-B: pressure=1014hPa
aggregated: sensor-C: humidity=61%
aggregated: sensor-B: pressure=1015hPa
aggregated: sensor-C: humidity=62%
--- sensor-C stream ended ---
aggregated: sensor-B: pressure=1016hPa
aggregated: sensor-B: pressure=1017hPa
--- sensor-B stream ended ---
all sensor streams closed
```
Sensor A finishes first, but the aggregator keeps reading from B and C until they also close.

## Step 4 -- Merge with Cancellation

Add a done channel so the consumer can cancel all forwarding goroutines without waiting for sources to close. This is essential when you want to stop aggregation early (e.g., after collecting enough data or on shutdown).

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func mergeWithDone(done <-chan struct{}, streams ...<-chan string) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	forward := func(ch <-chan string) {
		defer wg.Done()
		for event := range ch {
			select {
			case <-done:
				return
			case out <- event:
			}
		}
	}

	wg.Add(len(streams))
	for _, ch := range streams {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	done := make(chan struct{})

	// Create 3 streams that produce events indefinitely.
	streams := make([]<-chan string, 3)
	names := []string{"api-gateway", "auth-service", "payment-service"}
	for i := 0; i < 3; i++ {
		ch := make(chan string)
		streams[i] = ch
		go func(name string, c chan<- string) {
			seq := 0
			for {
				select {
				case <-done:
					close(c)
					return
				case c <- fmt.Sprintf("%s: log-entry-%d", name, seq):
					seq++
					time.Sleep(50 * time.Millisecond)
				}
			}
		}(names[i], ch)
	}

	merged := mergeWithDone(done, streams...)

	// Consume 10 events, then cancel.
	for i := 0; i < 10; i++ {
		fmt.Println(<-merged)
	}

	close(done)
	for range merged {
	} // Drain in-flight events.
	fmt.Println("aggregation cancelled and cleaned up")
}
```

The forward goroutines check `done` on every send to `out`. When `done` is closed, they exit immediately. The drain loop after `close(done)` consumes any values that were in flight.

### Verification
```
api-gateway: log-entry-0
auth-service: log-entry-0
payment-service: log-entry-0
api-gateway: log-entry-1
auth-service: log-entry-1
payment-service: log-entry-1
api-gateway: log-entry-2
auth-service: log-entry-2
payment-service: log-entry-2
api-gateway: log-entry-3
aggregation cancelled and cleaned up
```
Exactly 10 events appear, then clean shutdown. No goroutine leaks.

## Step 5 -- Tagged Events for Source Identification

In a real aggregator, you need to know which source produced each event. Wrap events in a struct that carries the source identifier.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Event struct {
	Source  string
	Payload string
}

func mergeTagged(done <-chan struct{}, streams ...<-chan Event) <-chan Event {
	out := make(chan Event)
	var wg sync.WaitGroup

	forward := func(ch <-chan Event) {
		defer wg.Done()
		for event := range ch {
			select {
			case <-done:
				return
			case out <- event:
			}
		}
	}

	wg.Add(len(streams))
	for _, ch := range streams {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func makeSource(done <-chan struct{}, name string, count int, interval time.Duration) <-chan Event {
	ch := make(chan Event)
	go func() {
		defer close(ch)
		for i := 0; i < count; i++ {
			event := Event{
				Source:  name,
				Payload: fmt.Sprintf("entry-%d", i),
			}
			select {
			case <-done:
				return
			case ch <- event:
			}
			time.Sleep(interval)
		}
	}()
	return ch
}

func main() {
	done := make(chan struct{})

	streams := []<-chan Event{
		makeSource(done, "nginx-access", 3, 20*time.Millisecond),
		makeSource(done, "app-errors", 2, 40*time.Millisecond),
		makeSource(done, "audit-log", 4, 30*time.Millisecond),
	}

	for event := range mergeTagged(done, streams...) {
		fmt.Printf("[%s] %s\n", event.Source, event.Payload)
	}
	fmt.Println("all sources exhausted")
}
```

### Verification
```
[nginx-access] entry-0
[app-errors] entry-0
[audit-log] entry-0
[nginx-access] entry-1
[audit-log] entry-1
[nginx-access] entry-2
[app-errors] entry-1
[audit-log] entry-2
[audit-log] entry-3
all sources exhausted
```
Each event carries its source name, making it easy to filter, route, or aggregate by origin.

## Common Mistakes

### 1. Closing the Output Channel in Forward Goroutines
Only one goroutine should close `out`, and only after ALL forwarders have finished. This is the WaitGroup goroutine's job. If a forwarder closes `out`, other forwarders will panic when they try to send:

```go
// BAD: multiple goroutines might close out.
forward := func(ch <-chan string) {
    for event := range ch {
        out <- event
    }
    close(out) // PANIC if another forwarder is still sending.
}

// GOOD: one goroutine closes out after all forwarders finish.
go func() {
    wg.Wait()
    close(out)
}()
```

### 2. Not Closing Source Channels
The forwarder uses `range`, which blocks until the source closes. If a source never closes and there is no done channel, the forwarder goroutine leaks forever.

### 3. Forgetting to Drain After Cancellation
If forwarding goroutines sent values to `out` before noticing `done`, those values sit in the channel. Without draining, the goroutines block on the send forever:

```go
close(done)
// REQUIRED: drain in-flight values.
for range merged {
}
```

### 4. Capturing the Loop Variable (Go < 1.22)
In Go versions before 1.22, the loop variable in a `for range` is shared across iterations. Passing it as a function argument avoids the issue:

```go
// SAFE: ch is a function parameter, not a closure capture.
for _, ch := range streams {
    go forward(ch) // ch is passed by value.
}
```

## Verify What You Learned

- [ ] Can you explain why each source needs its own forwarding goroutine?
- [ ] Can you describe the role of the WaitGroup goroutine and why it must be separate?
- [ ] Can you explain why draining is necessary after cancellation?
- [ ] Can you extend this to merge channels of different types using generics?

## What's Next
You have completed the select and multiplexing section. The next section covers sync primitives (`sync.Mutex`, `sync.RWMutex`, `sync.Once`, `sync.Pool`) for shared-state concurrency.

## Summary
Multiplexing N channels into one uses the fan-in pattern: one goroutine per source forwards events to a shared output channel. A `sync.WaitGroup` tracks forwarders and a separate goroutine closes the output channel when all forwarders are done. In a real-time data aggregator scenario (log streams, sensor feeds, API events), this lets you merge events from a dynamic number of sources into a single ordered stream. Adding a done channel enables cancellation when you need to stop aggregation early. Sources finishing at different times is handled naturally -- the merge keeps reading from active sources.

## Reference
- [Go Concurrency Patterns: Fan-in](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
- [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup)
