# 8. time.Ticker Periodic Goroutines

<!--
difficulty: intermediate
concepts: [time-ticker, periodic-tasks, goroutine-lifecycle, ticker-stop]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, select, context]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with goroutines, channels, and `select`
- Understanding of `context.Context`

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `time.Ticker` delivers periodic events on a channel
- **Apply** tickers for background tasks like health checks, metrics, and cleanup
- **Identify** the importance of stopping tickers and managing goroutine lifecycle

## Why time.Ticker

Many production systems need periodic tasks: flushing metrics every 10 seconds, checking health every 30 seconds, or cleaning up expired sessions every minute. `time.Ticker` provides a channel that delivers a value at regular intervals.

Unlike `time.After` (one-shot), a ticker repeats until stopped. Unlike `time.Sleep` in a loop, a ticker does not drift over time -- it compensates for the time taken by the task.

## Step 1 -- Basic Ticker

```bash
mkdir -p ~/go-exercises/ticker
cd ~/go-exercises/ticker
go mod init ticker
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	done := make(chan struct{})
	go func() {
		time.Sleep(1 * time.Second)
		close(done)
	}()

	count := 0
	for {
		select {
		case t := <-ticker.C:
			count++
			fmt.Printf("Tick %d at %s\n", count, t.Format("15:04:05.000"))
		case <-done:
			fmt.Printf("Done after %d ticks\n", count)
			return
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: About 5 ticks at 200ms intervals, then "Done after 5 ticks" (exact count may vary by 1).

## Step 2 -- Periodic Background Task

```go
package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

type MetricsCollector struct {
	requestCount atomic.Int64
}

func (mc *MetricsCollector) RecordRequest() {
	mc.requestCount.Add(1)
}

func (mc *MetricsCollector) StartReporting(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				count := mc.requestCount.Swap(0)
				fmt.Printf("[metrics] %d requests in last %v\n", count, interval)
			case <-ctx.Done():
				fmt.Println("[metrics] reporter stopped")
				return
			}
		}
	}()
}

func main() {
	mc := &MetricsCollector{}
	ctx, cancel := context.WithTimeout(context.Background(), 550*time.Millisecond)
	defer cancel()

	mc.StartReporting(ctx, 100*time.Millisecond)

	// Simulate requests
	for i := 0; i < 50; i++ {
		mc.RecordRequest()
		time.Sleep(10 * time.Millisecond)
	}

	<-ctx.Done()
	time.Sleep(20 * time.Millisecond) // Let reporter print final message
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Metrics printed every 100ms showing request counts, then "reporter stopped".

## Step 3 -- Multiple Tickers with Different Intervals

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()

	health := time.NewTicker(500 * time.Millisecond)
	cleanup := time.NewTicker(300 * time.Millisecond)
	defer health.Stop()
	defer cleanup.Stop()

	for {
		select {
		case <-health.C:
			fmt.Printf("[%s] Health check: OK\n", time.Now().Format("04:05.000"))
		case <-cleanup.C:
			fmt.Printf("[%s] Cleanup: expired sessions removed\n", time.Now().Format("04:05.000"))
		case <-ctx.Done():
			fmt.Println("Shutting down")
			return
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: Interleaved health checks (every 500ms) and cleanup tasks (every 300ms) for about 1 second.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Forgetting `ticker.Stop()` | Ticker goroutine leaks and continues firing |
| Using `time.Tick()` (no way to stop) | Only suitable for top-level programs that run forever |
| Blocking inside the ticker handler | Ticks pile up; the ticker does not skip missed ticks |

## Verify What You Learned

1. Create a ticker that prints every 250ms and stops after exactly 4 ticks using a counter
2. Combine a ticker with a context timeout and verify the goroutine exits cleanly

## What's Next

Continue to [09 - Or-Channel Pattern](../09-or-channel-pattern/09-or-channel-pattern.md) to learn the first-to-complete pattern.

## Summary

- `time.NewTicker(d)` returns a ticker that sends on `.C` every `d` duration
- Always call `ticker.Stop()` to release the ticker's resources
- Use `select` with `ticker.C` and a done/context channel for clean shutdown
- Tickers do not drift; they compensate for handler execution time
- Use `time.Tick()` only in long-running main functions where cleanup is not needed

## Reference

- [time.Ticker documentation](https://pkg.go.dev/time#Ticker)
- [time.NewTicker documentation](https://pkg.go.dev/time#NewTicker)
