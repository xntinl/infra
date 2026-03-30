---
difficulty: advanced
concepts: [or-channel, speculative execution, cancellation, select, redundant work]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channels, select, context, done channel pattern]
---

# 7. Or-Channel: First to Finish

## Learning Objectives
After completing this exercise, you will be able to:
- **Implement** the or-channel pattern to race multiple goroutines
- **Cancel** losing goroutines after the first result arrives
- **Apply** speculative execution for latency optimization
- **Analyze** the trade-offs of redundant work vs lower tail latency

## Why Or-Channel (First to Finish)

The or-channel pattern runs the same (or equivalent) work in multiple goroutines and takes whichever result comes first, canceling the rest. This is speculative execution: you trade extra CPU for lower latency by hedging your bets.

Consider a real scenario: your service reads user profiles from a database with 3 replicas. Most of the time, any replica responds in 5ms. But occasionally, one replica experiences a garbage collection pause or disk contention, causing a 500ms spike. If you query just one replica, your p99 latency is 500ms. If you query all 3 replicas and take the fastest response, your p99 drops dramatically -- the probability that ALL 3 replicas are slow simultaneously is extremely low. Google famously uses this pattern to reduce tail latency at scale.

The pattern has three parts: launch N goroutines doing equivalent work, select the first result from any of them, and cancel the rest immediately. Without proper cancellation, the losing goroutines waste resources running to completion.

```
  Database Replica Racing

  query ---> replica 1 (5ms)       --+
         --> replica 2 (500ms GC!) --+--> take first (5ms), cancel rest
         --> replica 3 (8ms)       --+

  User sees 5ms instead of 500ms. Tail latency reduced by 100x.
```

## Step 1 -- Basic Replica Race

Create multiple goroutines that query database replicas with different simulated latencies and take the fastest.

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

type QueryResult struct {
	Data    string
	Replica string
	Latency time.Duration
}

func main() {
	fmt.Println("=== Database Replica Race ===")
	fmt.Println()

	replicas := []string{"us-east-1", "us-west-2", "eu-west-1"}
	ch := make(chan QueryResult, len(replicas))

	for _, replica := range replicas {
		go func(name string) {
			// Simulate variable latency: usually fast, occasionally slow (GC pause)
			latency := time.Duration(5+rand.Intn(15)) * time.Millisecond
			if rand.Float64() < 0.3 { // 30% chance of slow response
				latency = time.Duration(200+rand.Intn(300)) * time.Millisecond
			}

			time.Sleep(latency)
			ch <- QueryResult{
				Data:    fmt.Sprintf("user_profile{name:alice,id:42}"),
				Replica: name,
				Latency: latency,
			}
		}(replica)
	}

	// Take the first response
	winner := <-ch
	fmt.Printf("  Winner: %s responded in %v\n", winner.Replica, winner.Latency)
	fmt.Printf("  Data: %s\n", winner.Data)
}
```

The channel is buffered so that losing goroutines can send their results without blocking, even after the consumer has moved on. Without the buffer, losers would leak.

### Intermediate Verification
```bash
go run main.go
```
Expected: the fastest replica wins, varying between runs:
```
=== Database Replica Race ===

  Winner: us-west-2 responded in 8ms
  Data: user_profile{name:alice,id:42}
```

## Step 2 -- Race with Cancellation

Use `context.WithCancel` to properly cancel losing replicas and free their resources.

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

type QueryResult struct {
	Data    string
	Replica string
	Latency time.Duration
}

func queryReplica(ctx context.Context, replica string) (QueryResult, error) {
	latency := time.Duration(5+rand.Intn(15)) * time.Millisecond
	if rand.Float64() < 0.3 {
		latency = time.Duration(200+rand.Intn(300)) * time.Millisecond
	}

	select {
	case <-time.After(latency):
		return QueryResult{
			Data:    "user_profile{name:alice,id:42}",
			Replica: replica,
			Latency: latency,
		}, nil
	case <-ctx.Done():
		fmt.Printf("  [%s] canceled after %v (was going to take %v)\n",
			replica, time.Since(time.Now()), latency)
		return QueryResult{}, ctx.Err()
	}
}

func main() {
	fmt.Println("=== Replica Race with Cancellation ===")
	fmt.Println()

	replicas := []string{"us-east-1", "us-west-2", "eu-west-1"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan QueryResult, 1)

	for _, replica := range replicas {
		go func(name string) {
			result, err := queryReplica(ctx, name)
			if err != nil {
				return
			}
			select {
			case ch <- result:
			case <-ctx.Done():
			}
		}(replica)
	}

	winner := <-ch
	cancel() // cancel all remaining replicas
	fmt.Printf("  Winner: %s in %v\n", winner.Replica, winner.Latency)
	fmt.Printf("  Data: %s\n\n", winner.Data)

	time.Sleep(20 * time.Millisecond) // let cancel messages print
	fmt.Println("  Losing replicas were canceled and their goroutines exited cleanly.")
}
```

After receiving the first result, `cancel()` triggers `ctx.Done()` in all goroutines, causing them to exit cleanly.

### Intermediate Verification
```bash
go run main.go
```
Expected: one winner, other replicas report cancellation:
```
=== Replica Race with Cancellation ===

  Winner: eu-west-1 in 7ms
  Data: user_profile{name:alice,id:42}

  [us-east-1] canceled (was going to take 245ms)
  Losing replicas were canceled and their goroutines exited cleanly.
```

## Step 3 -- Measure Tail Latency Improvement

Run multiple queries to show the statistical improvement from racing replicas.

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"
)

func queryWithLatency(ctx context.Context) time.Duration {
	latency := time.Duration(5+rand.Intn(15)) * time.Millisecond
	if rand.Float64() < 0.2 {
		latency = time.Duration(200+rand.Intn(300)) * time.Millisecond
	}
	select {
	case <-time.After(latency):
		return latency
	case <-ctx.Done():
		return 0
	}
}

func percentile(latencies []time.Duration, p float64) time.Duration {
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	idx := int(float64(len(latencies)) * p)
	if idx >= len(latencies) {
		idx = len(latencies) - 1
	}
	return latencies[idx]
}

func main() {
	const iterations = 100

	// Single replica: take whatever latency you get
	fmt.Println("=== Tail Latency Comparison (100 queries) ===")
	fmt.Println()
	singleLatencies := make([]time.Duration, iterations)
	for i := 0; i < iterations; i++ {
		ctx := context.Background()
		singleLatencies[i] = queryWithLatency(ctx)
	}

	// Three replicas: race and take the fastest
	racedLatencies := make([]time.Duration, iterations)
	for i := 0; i < iterations; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan time.Duration, 3)
		for r := 0; r < 3; r++ {
			go func() {
				lat := queryWithLatency(ctx)
				if lat > 0 {
					ch <- lat
				}
			}()
		}
		racedLatencies[i] = <-ch
		cancel()
	}

	fmt.Println("  Single replica:")
	fmt.Printf("    p50:  %v\n", percentile(singleLatencies, 0.50))
	fmt.Printf("    p90:  %v\n", percentile(singleLatencies, 0.90))
	fmt.Printf("    p99:  %v\n", percentile(singleLatencies, 0.99))
	fmt.Printf("    max:  %v\n\n", percentile(singleLatencies, 1.0))

	fmt.Println("  Three replicas (raced):")
	fmt.Printf("    p50:  %v\n", percentile(racedLatencies, 0.50))
	fmt.Printf("    p90:  %v\n", percentile(racedLatencies, 0.90))
	fmt.Printf("    p99:  %v\n", percentile(racedLatencies, 0.99))
	fmt.Printf("    max:  %v\n\n", percentile(racedLatencies, 1.0))

	fmt.Println("  Racing replicas dramatically reduces tail latency (p90, p99).")
	fmt.Println("  The cost is 3x the queries, but user-facing latency improves significantly.")
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected: p50 similar, but p90/p99 dramatically lower with racing:
```
=== Tail Latency Comparison (100 queries) ===

  Single replica:
    p50:  12ms
    p90:  298ms
    p99:  467ms
    max:  489ms

  Three replicas (raced):
    p50:  8ms
    p90:  14ms
    p99:  18ms
    max:  22ms

  Racing replicas dramatically reduces tail latency (p90, p99).
  The cost is 3x the queries, but user-facing latency improves significantly.
```

## Step 4 -- Reusable Or-Channel Function

Implement a reusable `or` function that takes multiple `<-chan struct{}` channels and returns a channel that closes when any of them closes. This is the general-purpose "first signal wins" combiner, useful for combining multiple cancellation signals.

```go
package main

import (
	"fmt"
	"time"
)

func or(channels ...<-chan struct{}) <-chan struct{} {
	switch len(channels) {
	case 0:
		return nil
	case 1:
		return channels[0]
	}

	orDone := make(chan struct{})
	go func() {
		defer close(orDone)
		switch len(channels) {
		case 2:
			select {
			case <-channels[0]:
			case <-channels[1]:
			}
		default:
			select {
			case <-channels[0]:
			case <-channels[1]:
			case <-channels[2]:
			case <-or(append(channels[3:], orDone)...):
			}
		}
	}()
	return orDone
}

func replicaSignal(replica string, latency time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		time.Sleep(latency)
		fmt.Printf("  [%s] responded in %v\n", replica, latency)
	}()
	return ch
}

func main() {
	fmt.Println("=== Or-Channel: First Replica Wins ===")
	fmt.Println()

	start := time.Now()
	<-or(
		replicaSignal("us-east-1", 300*time.Millisecond),
		replicaSignal("us-west-2", 50*time.Millisecond),  // fastest
		replicaSignal("eu-west-1", 150*time.Millisecond),
	)
	fmt.Printf("\n  First response received after %v\n",
		time.Since(start).Round(time.Millisecond))
}
```

### Intermediate Verification
```bash
go run main.go
```
```
=== Or-Channel: First Replica Wins ===

  [us-west-2] responded in 50ms

  First response received after 50ms
```

## Common Mistakes

### Unbuffered Channel Causes Goroutine Leaks
**Wrong:**
```go
package main

import "fmt"

func main() {
	type result struct{ replica string }
	ch := make(chan result) // unbuffered
	for i := 0; i < 3; i++ {
		go func(id int) {
			ch <- result{fmt.Sprintf("replica-%d", id)}
		}(i)
	}
	winner := <-ch
	fmt.Println(winner)
	// two goroutines are stuck trying to send forever
}
```
**What happens:** The losing goroutines block on send forever because nobody reads their values.

**Fix:** Either buffer the channel to hold all results, or use context cancellation to stop losers.

### Not Canceling Losing Goroutines
**Wrong:**
```go
winner := <-ch
// forget to cancel -- losing goroutines run to completion
```
**What happens:** Losing goroutines waste CPU, memory, and database connections completing work whose result is discarded.

**Fix:** Use `context.WithCancel` and call `cancel()` after receiving the first result.

### Race Condition on the Result Channel
If multiple goroutines finish at the same instant, only one value is read. The others either block (unbuffered) or sit in the buffer (buffered). This is correct behavior -- you wanted only the first -- but make sure your channel and cancellation strategy handle it.

## Verify What You Learned

Run `go run main.go` and verify:
- Basic race: one winning replica reported
- Race with cancellation: winner selected, losers canceled cleanly
- Tail latency: p90/p99 dramatically lower with 3 replicas vs 1
- Or-channel function: first signal received in ~50ms

## What's Next
Continue to [08-tee-channel-split-stream](../08-tee-channel-split-stream/08-tee-channel-split-stream.md) to learn how to duplicate a channel stream for parallel processing.

## Summary
- The or-channel pattern races N goroutines and takes the first result
- Buffer result channels or use cancellation to prevent goroutine leaks from losers
- `context.WithCancel` provides clean cancellation of losing goroutines
- The recursive `or` function combines N signal channels into one
- Racing replicas trades CPU for dramatically lower tail latency (p90, p99)
- Always cancel remaining work after receiving the winning result

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
- [Advanced Go Concurrency Patterns](https://www.youtube.com/watch?v=QDDwwePbDtw)
- [The Tail at Scale (Google)](https://research.google/pubs/pub40801/) -- the paper motivating redundant requests
