---
difficulty: basic
concepts: [select, default-case, non-blocking-operations, polling]
tools: [go]
estimated_time: 20m
bloom_level: apply
---

# 2. Select with Default

## Learning Objectives
- **Use** the `default` case to make channel operations non-blocking
- **Implement** a non-blocking receive and a non-blocking send
- **Build** a polling loop using `select` with `default`
- **Recognize** the CPU cost of misusing `default`

## Why Default in Select

A plain `select` blocks until one of its channel operations can proceed. This is usually what you want, but sometimes blocking is unacceptable. Consider a cache layer in your application: you want to check if a precomputed value is available in the cache, but if nothing is there, you need to compute the value immediately instead of waiting. You cannot afford to block.

The `default` case transforms `select` from a blocking multiplexer into a non-blocking probe. When present, `default` executes immediately if no other case is ready. This gives you a try-operation: "receive if there is something, otherwise continue."

This pattern appears in rate limiters (try to acquire a token, skip if none available), logging pipelines (send the log entry, drop it if the buffer is full), metrics collectors (poll for new data without stalling), and any situation where blocking would compromise responsiveness.

## Step 1 -- Non-Blocking Cache Read

Try to read from a cache channel. If a precomputed value is available, use it. If not, fall through to compute the value on the spot.

```go
package main

import "fmt"

func tryReadCache(cache <-chan string) (string, bool) {
	select {
	case value := <-cache:
		return value, true
	default:
		return "", false
	}
}

func computeValue() string {
	return "computed-result-42"
}

func main() {
	cache := make(chan string, 1)

	// Cache miss: channel is empty, so we compute.
	if value, hit := tryReadCache(cache); hit {
		fmt.Println("cache hit:", value)
	} else {
		fmt.Println("cache miss: computing value...")
		computed := computeValue()
		fmt.Println("computed:", computed)
	}

	// Simulate a background worker that fills the cache.
	cache <- "precomputed-result-42"

	// Cache hit: channel has a value.
	if value, hit := tryReadCache(cache); hit {
		fmt.Println("cache hit:", value)
	} else {
		fmt.Println("cache miss: computing value...")
	}
}
```

The first `select` hits `default` because the cache channel is empty -- this is a cache miss. The second `select` receives the precomputed value -- a cache hit. The caller never blocks in either case.

### Verification
```
cache miss: computing value...
computed: computed-result-42
cache hit: precomputed-result-42
```

## Step 2 -- Non-Blocking Metrics Send (Try-Send Pattern)

A metrics collector produces data points that should be sent to an aggregation channel. If the aggregator is overwhelmed (buffer full), the metric is dropped rather than stalling the collector. This is the "try-send" pattern.

```go
package main

import "fmt"

const metricsBufferSize = 2

func trySendMetric(buffer chan<- string, metric string) bool {
	select {
	case buffer <- metric:
		return true
	default:
		return false
	}
}

func drainMetrics(buffer <-chan string, count int) {
	for i := 0; i < count; i++ {
		fmt.Println("collected:", <-buffer)
	}
}

func main() {
	metricsBuffer := make(chan string, metricsBufferSize)

	// Buffer has room: both sends succeed.
	fmt.Println("sent:", trySendMetric(metricsBuffer, "cpu_usage=72%"))
	fmt.Println("sent:", trySendMetric(metricsBuffer, "mem_usage=85%"))

	// Buffer is full: this metric is dropped.
	fmt.Println("sent:", trySendMetric(metricsBuffer, "disk_io=40%"))

	// Drain what was buffered.
	drainMetrics(metricsBuffer, metricsBufferSize)
}
```

This is the "fire and forget" pattern. It is used when dropping a data point is acceptable -- non-critical metrics, overflow logs, or sampled telemetry. The alternative (blocking) would cause the entire collector to stall when the aggregator falls behind.

### Verification
```
sent: true
sent: true
sent: false
collected: cpu_usage=72%
collected: mem_usage=85%
```

## Step 3 -- Polling Metrics Collector

Build a metrics collector that periodically polls a data channel without blocking. Between polls, it does useful work (processing the backlog, running calculations). This creates a cooperative multitasking loop.

```go
package main

import (
	"fmt"
	"time"
)

const (
	maxPollAttempts   = 5
	pollWorkInterval  = 100 * time.Millisecond
	externalDataDelay = 250 * time.Millisecond
)

func startExternalProducer(dataFeed chan<- string) {
	go func() {
		time.Sleep(externalDataDelay)
		dataFeed <- "metric_batch_ready"
	}()
}

func pollForData(dataFeed <-chan string, maxAttempts int) bool {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		select {
		case data := <-dataFeed:
			fmt.Println("received:", data)
			return true
		default:
			fmt.Printf("poll %d: no data yet, processing backlog...\n", attempt)
			time.Sleep(pollWorkInterval)
		}
	}
	return false
}

func main() {
	dataFeed := make(chan string, 1)

	startExternalProducer(dataFeed)

	if !pollForData(dataFeed, maxPollAttempts) {
		fmt.Println("gave up waiting")
	}
}
```

Each iteration checks the data channel. If nothing is there, the collector does useful work and checks again. After ~250ms the external system delivers data.

### Verification
```
poll 0: no data yet, processing backlog...
poll 1: no data yet, processing backlog...
received: metric_batch_ready
```
The exact iteration count depends on scheduling, but you should see 2-3 "no data yet" lines followed by the received message.

## Step 4 -- Probing Multiple Caches

Combine `default` with multiple channel cases to check several cache layers without blocking on any of them.

```go
package main

import "fmt"

type CacheLayer struct {
	Name    string
	Channel chan string
}

func probeCaches(layers []CacheLayer) (value string, hitLayer string, found bool) {
	// Build a select dynamically is not possible, so we probe with a
	// three-layer select. For a real N-layer cache, iterate sequentially.
	select {
	case val := <-layers[0].Channel:
		return val, layers[0].Name, true
	case val := <-layers[1].Channel:
		return val, layers[1].Name, true
	case val := <-layers[2].Channel:
		return val, layers[2].Name, true
	default:
		return "", "", false
	}
}

func main() {
	layers := []CacheLayer{
		{Name: "L1", Channel: make(chan string, 1)},
		{Name: "L2", Channel: make(chan string, 1)},
		{Name: "DB cache", Channel: make(chan string, 1)},
	}

	// All caches empty: full miss.
	if value, layer, found := probeCaches(layers); found {
		fmt.Printf("%s hit: %s\n", layer, value)
	} else {
		fmt.Println("all caches empty, querying database directly")
	}

	// Populate L2 and try again.
	layers[1].Channel <- "user:1234:profile"

	if value, layer, found := probeCaches(layers); found {
		fmt.Printf("%s hit: %s\n", layer, value)
	} else {
		fmt.Println("all caches empty, querying database directly")
	}
}
```

### Verification
```
all caches empty, querying database directly
L2 hit: user:1234:profile
```

## Step 5 -- Draining a Metrics Buffer

Use `select` + `default` in a loop to flush all buffered metrics without blocking when the buffer is empty.

```go
package main

import "fmt"

const metricsBufferCapacity = 10

func flushMetricsBuffer(buffer <-chan string) int {
	flushed := 0
	for {
		select {
		case metric := <-buffer:
			fmt.Println("flushed:", metric)
			flushed++
		default:
			return flushed
		}
	}
}

func main() {
	metricsBuffer := make(chan string, metricsBufferCapacity)
	metricsBuffer <- "request_count=142"
	metricsBuffer <- "error_rate=0.02"
	metricsBuffer <- "p99_latency=230ms"
	metricsBuffer <- "active_connections=84"

	flushed := flushMetricsBuffer(metricsBuffer)
	fmt.Printf("flush complete: %d metrics sent to aggregator\n", flushed)
}
```

### Verification
```
flushed: request_count=142
flushed: error_rate=0.02
flushed: p99_latency=230ms
flushed: active_connections=84
flush complete: 4 metrics sent to aggregator
```

## Common Mistakes

### 1. Using Default When You Should Block
Adding `default` to every `select` turns blocking waits into busy loops that burn CPU. Only use `default` when you genuinely need non-blocking behavior.

```go
package main

import "fmt"

const busySpinLimit = 1000000

func main() {
	work := make(chan int)

	// BAD: this spins at 100% CPU doing nothing useful.
	// Without the iteration limit, this would run forever.
	spins := 0
	for i := 0; i < busySpinLimit; i++ {
		select {
		case value := <-work:
			fmt.Println(value)
			return
		default:
			spins++
			// No work, no sleep — pure CPU waste.
		}
	}
	fmt.Printf("spun %d times doing nothing\n", spins)
}
```

Expected output:
```
spun 1000000 times doing nothing
```

### 2. Polling Without Sleep or Work
A `for { select { default: } }` with no work in the default case is a tight spin loop. It will consume 100% of a CPU core. Always include meaningful work or a small sleep in the default body.

### 3. Confusing "Non-Blocking" with "Instant"
The `default` case makes the `select` non-blocking, but the goroutine still takes time to execute the default body. It is not a zero-cost operation.

## Verify What You Learned

- [ ] Can you explain the difference between `select` with and without `default`?
- [ ] Can you describe a scenario where a non-blocking send is the right choice?
- [ ] Can you identify the risk of using `default` inside a tight loop?
- [ ] Can you write a drain loop using `select` + `default`?

## What's Next
In the next exercise, you will learn how to use `time.After` and `time.NewTimer` to add timeout behavior to `select` statements.

## Summary
The `default` case in `select` makes channel operations non-blocking. A non-blocking receive checks a cache channel and continues immediately if empty -- the cache miss path. A non-blocking send drops the metric if the aggregator buffer is full -- the try-send pattern. Combined with a loop, `select` + `default` creates a polling pattern for metrics collection. Use it deliberately -- unnecessary `default` cases turn efficient blocking into wasteful CPU spinning.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Go by Example: Non-Blocking Channel Operations](https://gobyexample.com/non-blocking-channel-operations)
